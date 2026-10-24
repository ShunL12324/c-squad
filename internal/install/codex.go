package install

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// Codex reads skills from ~/.agents/skills. csquad sets Codex up only when
// $CODEX_HOME (default ~/.codex) exists, and takes its parts back on update
// when it no longer does. Codex has no status line for task chips.

func (o Options) userHome() string {
	if h := o.Env.Get("HOME"); h != "" && !proc.Windows {
		return h
	}
	if h := o.Env.Get("USERPROFILE"); h != "" && proc.Windows {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

func (o Options) codexHome() string {
	if o.CodexHome != "" {
		return o.CodexHome
	}
	if h := o.Env.Get("CODEX_HOME"); h != "" {
		return h
	}
	return filepath.Join(o.userHome(), ".codex")
}

func (o Options) agentsDir() string {
	if o.AgentsDir != "" {
		return o.AgentsDir
	}
	return filepath.Join(o.userHome(), ".agents")
}

// codexPlan is what install will write for Codex; nil files when
// $CODEX_HOME does not exist.
type codexPlan struct {
	dir   string            // the agents dir
	files map[string][]byte // agents-dir relative path → content
}

// planCodex renders the Codex skills and refuses, before anything is
// written, symlinked skill files and dangling directory links.
func (o Options) planCodex() (codexPlan, error) {
	p := codexPlan{dir: o.agentsDir()}
	if st, err := os.Stat(o.codexHome()); err != nil || !st.IsDir() {
		return p, nil
	}
	p.files = o.skillFiles("codex")
	dirs := map[string]bool{p.dir: true, filepath.Join(p.dir, "skills"): true}
	for rel := range p.files {
		f := filepath.Join(p.dir, filepath.FromSlash(rel))
		if isLink(f) {
			return p, fmt.Errorf("%s is a symlink; csquad manages this file itself, remove the link first, nothing was changed", f)
		}
		dirs[filepath.Dir(f)] = true
	}
	for d := range dirs {
		if isLink(d) {
			if _, err := os.Stat(d); err != nil {
				return p, fmt.Errorf("%s is a dangling symlink; fix or remove it first, nothing was changed", d)
			}
		}
	}
	return p, nil
}

// installCodex writes the Codex skills (or removes the ones a previous
// install wrote when $CODEX_HOME is gone) and records them in m.
func (o Options) installCodex(p codexPlan, m *Manifest, old Manifest) error {
	oldDir := old.AgentsDir
	if oldDir == "" {
		oldDir = p.dir
	}
	if p.files == nil {
		if len(old.CodexFiles) > 0 {
			o.removeCodex(oldDir, old.CodexFiles)
			o.log("Codex: %s does not exist; removed the Codex skills from %s", o.codexHome(), filepath.Join(oldDir, "skills"))
		}
		return nil
	}
	previous := old.CodexFiles
	if oldDir != p.dir {
		// The home moved: take the old copies back, write fresh ones.
		o.removeCodex(oldDir, previous)
		previous = nil
	}
	hashes, err := o.writeManaged(p.dir, p.files, previous)
	if err != nil {
		return err
	}
	m.AgentsDir, m.CodexFiles = p.dir, hashes
	// The Stop hook in $CODEX_HOME/hooks.json: installCodexHook.
	o.log("Codex: skills in %s (run csquad outside Codex's sandbox)", filepath.Join(p.dir, "skills"))
	return nil
}

func (o Options) removeCodex(dir string, files map[string]string) {
	for rel, hash := range files {
		o.removeFrom(dir, rel, hash)
	}
	rmdirIfEmpty(filepath.Join(dir, "skills"))
	rmdirIfEmpty(dir)
}

// uninstallCodex removes what install wrote for Codex.
func (o Options) uninstallCodex(m Manifest) {
	if len(m.CodexFiles) == 0 {
		return
	}
	o.removeCodex(m.AgentsDir, m.CodexFiles)
}

// statusCodex prints the Codex part of an installation.
func (o Options) statusCodex(m Manifest) {
	if len(m.CodexFiles) == 0 {
		if st, err := os.Stat(o.codexHome()); err == nil && st.IsDir() {
			o.log("Codex:       not set up (%s exists; run csquad update)", o.codexHome())
		}
		return
	}
	o.log("Codex:       skills in %s", filepath.Join(m.AgentsDir, "skills"))
	var rels []string
	for rel := range m.CodexFiles {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(m.AgentsDir, filepath.FromSlash(rel)))
		state := "ok"
		switch {
		case err != nil:
			state = "missing"
		case sha(b) != m.CodexFiles[rel]:
			state = "edited"
		}
		o.log("  %-8s %s", state, rel)
	}
}
