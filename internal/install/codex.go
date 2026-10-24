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

// agentsDirOf is the agents dir a manifest recorded, or today's.
func (o Options) agentsDirOf(m Manifest) string {
	if m.AgentsDir != "" {
		return m.AgentsDir
	}
	return o.agentsDir()
}

// codexPlan is what install will write for Codex; nil files when
// $CODEX_HOME does not exist.
type codexPlan struct {
	dir   string            // the agents dir
	files map[string][]byte // agents-dir relative path → content
	// shared: the agents dir's skills/ is the Claude skills directory, so
	// the shared copy written there serves Codex too and files is unused.
	shared bool
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
		if isDirLink(d) {
			if _, err := os.Stat(d); err != nil {
				return p, fmt.Errorf("%s is a dangling symlink; fix or remove it first, nothing was changed", d)
			}
		}
	}
	return p, nil
}

// codexCopies is what an install left below its agents dir: the Codex
// copies, or the shared copy, which is there too.
func codexCopies(m Manifest) map[string]string {
	if m.SharedSkills {
		return m.Files
	}
	return m.CodexFiles
}

// installCodex writes the Codex skills (or removes the ones a previous
// install wrote when $CODEX_HOME is gone) and records them in m. In a
// skills directory shared with Claude Code, install already wrote the one
// copy both read.
func (o Options) installCodex(p codexPlan, m *Manifest, old Manifest, known map[string][]string) error {
	oldDir := o.agentsDirOf(old)
	previous := codexCopies(old)
	if p.files == nil {
		if o.removeCodex(oldDir, previous, known) {
			o.log("Codex: %s does not exist; removed the Codex skills from %s", o.codexHome(), filepath.Join(oldDir, "skills"))
		}
		return nil
	}
	if p.shared {
		// Separate copies an earlier install wrote go, unless they are the
		// very files just written (today's directory, or a link to it).
		o.removeCodex(oldDir, old.CodexFiles, known)
		m.AgentsDir, m.SharedSkills = p.dir, true
		o.log("Codex: %s is the same directory as %s; wrote one copy of each skill, with the instructions for both Claude Code and Codex (run csquad outside Codex's sandbox)",
			filepath.Join(p.dir, "skills"), filepath.Join(o.ClaudeDir, "skills"))
		return nil
	}
	if oldDir != p.dir {
		// The home moved: take the old copies back, write fresh ones.
		o.removeCodex(oldDir, previous, known)
		previous = nil
	}
	hashes, err := o.writeManaged(p.dir, p.files, previous, known)
	if err != nil {
		return err
	}
	m.AgentsDir, m.CodexFiles = p.dir, hashes
	// The Stop hook in $CODEX_HOME/hooks.json: installCodexHook.
	o.log("Codex: skills in %s (run csquad outside Codex's sandbox)", filepath.Join(p.dir, "skills"))
	return nil
}

// removeCodex removes files csquad wrote below the agents dir dir and
// reports whether there were any. A file that is also the Claude skill of
// that name (a skills directory both share) is csquad's current copy and
// stays, and so does a shared skills directory, even emptied: the link to
// it would dangle.
func (o Options) removeCodex(dir string, files map[string]string, known map[string][]string) bool {
	removed := false
	for rel := range files {
		f := filepath.Join(dir, filepath.FromSlash(rel))
		if sameFile(f, filepath.Join(o.ClaudeDir, filepath.FromSlash(rel))) {
			continue
		}
		if _, err := os.Stat(f); err == nil {
			removed = true
		}
		o.removeFrom(dir, rel, known[rel])
	}
	if !o.sharedSkills(dir) {
		rmdirIfEmpty(filepath.Join(dir, "skills"))
		rmdirIfEmpty(dir)
	}
	return removed
}

// uninstallCodex removes what install wrote for Codex: in a shared skills
// directory nothing is left by now, unless the link went since.
func (o Options) uninstallCodex(m Manifest, known map[string][]string) {
	if files := codexCopies(m); len(files) > 0 {
		o.removeCodex(o.agentsDirOf(m), files, known)
	}
}

// statusCodex prints the Codex part of an installation.
func (o Options) statusCodex(m Manifest) {
	if m.SharedSkills {
		o.log("Codex:       reads the skills above (%s is the same directory)", filepath.Join(m.AgentsDir, "skills"))
		return
	}
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
