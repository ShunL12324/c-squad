package install

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

// ---- a skills directory Claude Code and Codex share -------------------------
//
// Users link ~/.claude/skills to ~/.agents/skills (a junction on Windows),
// or the other way round, so both agents read the same skills. csquad then
// writes one copy with both hosts' instructions.

func (e *env) claudeSkills() string { return filepath.Join(e.h.Claude, "skills") }
func (e *env) agentsSkills() string { return filepath.Join(e.h.Home, ".agents", "skills") }

// The layouts that share one directory.
var shareLayouts = []string{"claude→agents", "agents→claude", "both→third"}

// share sets Codex up and links the two skills directories as layout says.
func (e *env) share(layout string) {
	e.t.Helper()
	for _, d := range []string{filepath.Join(e.h.Home, ".codex"), e.h.Claude, filepath.Join(e.h.Home, ".agents")} {
		os.MkdirAll(d, 0o755)
	}
	switch layout {
	case "claude→agents":
		os.MkdirAll(e.agentsSkills(), 0o755)
		testutil.DirLink(e.t, e.agentsSkills(), e.claudeSkills())
	case "agents→claude":
		os.MkdirAll(e.claudeSkills(), 0o755)
		testutil.DirLink(e.t, e.claudeSkills(), e.agentsSkills())
	case "both→third":
		third := filepath.Join(e.h.Root, "dotfiles", "skills")
		os.MkdirAll(third, 0o755)
		testutil.DirLink(e.t, third, e.claudeSkills())
		testutil.DirLink(e.t, third, e.agentsSkills())
	default:
		e.t.Fatalf("layout %s", layout)
	}
}

// unshare removes the link(s) of layout, leaving each side its own
// directory: the linked side starts empty, or as a copy of the shared one.
func (e *env) unshare(layout string, copyFiles bool) {
	e.t.Helper()
	shared := files(snapshotFiles(e.t, e.claudeSkills()))
	var links []string
	switch layout {
	case "claude→agents":
		links = []string{e.claudeSkills()}
	case "agents→claude":
		links = []string{e.agentsSkills()}
	case "both→third":
		links = []string{e.claudeSkills(), e.agentsSkills()}
	}
	for _, l := range links {
		if err := os.Remove(l); err != nil {
			e.t.Fatal(err)
		}
		os.MkdirAll(l, 0o755)
		if copyFiles {
			for rel, b := range shared {
				f := filepath.Join(l, rel)
				os.MkdirAll(filepath.Dir(f), 0o755)
				os.WriteFile(f, b, 0o644)
			}
		}
	}
}

// snapshotFiles is every file below dir, which may be a link, relative
// path → content; every directory too, as "path/" → nil.
func snapshotFiles(t *testing.T, dir string) map[string][]byte {
	out := map[string][]byte{}
	fsys := os.DirFS(dir)
	fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil || p == ".":
		case d.IsDir():
			out[p+"/"] = nil
		default:
			b, _ := fs.ReadFile(fsys, p)
			out[p] = b
		}
		return nil
	})
	return out
}

// files drops the directories of a snapshot.
func files(snap map[string][]byte) map[string][]byte {
	for p := range snap {
		if strings.HasSuffix(p, "/") {
			delete(snap, p)
		}
	}
	return snap
}

func (e *env) manifest() Manifest {
	var m Manifest
	if !readJSON(filepath.Join(e.h.Csquad, "manifest.json"), &m) {
		e.t.Fatal("no manifest")
	}
	return m
}

func hashes(files map[string][]byte) map[string]string {
	out := map[string]string{}
	for rel, b := range files {
		out[rel] = sha(b)
	}
	return out
}

// wantSkills checks that dir holds exactly host's rendering of every skill
// file (relative to the Claude dir's "skills/").
func (e *env) wantSkills(dir, host string) {
	e.t.Helper()
	for rel, want := range e.o.skillFiles(host) {
		f := filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(rel, "skills/")))
		if got := e.read(f); got != string(want) {
			e.t.Errorf("%s is not the %s rendering:\n%s", f, host, got)
		}
	}
}

// noBak fails on any .bak file below the dirs.
func (e *env) noBak(dirs ...string) {
	e.t.Helper()
	for _, d := range dirs {
		for rel := range snapshotFiles(e.t, d) {
			if strings.HasSuffix(rel, ".bak") {
				e.t.Errorf("%s/%s written", d, rel)
			}
		}
	}
}

// userSkill is another tool's skill in the shared directory.
func (e *env) userSkill() string {
	f := filepath.Join(e.agentsSkills(), "mine", "SKILL.md")
	os.MkdirAll(filepath.Dir(f), 0o755)
	os.WriteFile(f, []byte("mine"), 0o644)
	return f
}

// linksIntact fails unless the layout's links and the shared directory are
// still there.
func (e *env) linksIntact(layout string) {
	e.t.Helper()
	links := map[string][]string{"claude→agents": {e.claudeSkills()}, "agents→claude": {e.agentsSkills()},
		"both→third": {e.claudeSkills(), e.agentsSkills()}}[layout]
	for _, l := range links {
		if !isDirLink(l) {
			e.t.Errorf("%s is no longer a link", l)
		}
	}
	if !sameFile(e.claudeSkills(), e.agentsSkills()) {
		e.t.Error("the shared skills directory is gone")
	}
}

func TestSharedSkillsDirGetsOneCopyWithBothHosts(t *testing.T) {
	for _, layout := range shareLayouts {
		t.Run(layout, func(t *testing.T) {
			e := setup(t)
			e.share(layout)
			mine := e.userSkill()
			out := e.install()
			if !strings.Contains(out, "Codex: "+e.agentsSkills()+" is the same directory as "+e.claudeSkills()+"; wrote one copy of each skill, with the instructions for both Claude Code and Codex") || strings.Contains(out, "edited") {
				t.Errorf("install:\n%s", out)
			}
			e.wantSkills(e.claudeSkills(), assets.Shared)
			watch := e.read(filepath.Join(e.claudeSkills(), "watch-task", "SKILL.md"))
			if !strings.Contains(watch, "**Claude Code:** Run the command below with the shell tool's") || !strings.Contains(watch, "**Codex:** Run the command below as a long-running command") {
				t.Errorf("watch-task:\n%s", watch)
			}
			m := e.manifest()
			if !m.SharedSkills || len(m.CodexFiles) != 0 || m.AgentsDir != filepath.Join(e.h.Home, ".agents") || len(m.Files) != len(assets.SkillFiles()) {
				t.Errorf("manifest: %+v", m)
			}
			Status(e.o)
			if out := e.out(); !strings.Contains(out, "Codex:       reads the skills above") || !strings.Contains(out, "ok       skills/watch-task/SKILL.md") || strings.Contains(out, "edited") {
				t.Errorf("status:\n%s", out)
			}
			// Reinstall and update change nothing.
			before := snapshot(e.h.Csquad)
			skills := snapshotFiles(t, e.claudeSkills())
			if out := e.update(); strings.Contains(out, "edited") {
				t.Errorf("update:\n%s", out)
			}
			if after := snapshot(e.h.Csquad); strings.Join(after, "\n") != strings.Join(before, "\n") || !reflect.DeepEqual(snapshotFiles(t, e.claudeSkills()), skills) {
				t.Error("update changed files")
			}
			// An edit is still the user's.
			edited := filepath.Join(e.claudeSkills(), "task-status", "SKILL.md")
			os.WriteFile(edited, []byte("my notes"), 0o644)
			if out := e.update(); !strings.Contains(out, "skills/task-status/SKILL.md was edited") || e.read(edited+".bak") != "my notes" {
				t.Errorf("update over an edit:\n%s", out)
			}
			os.Remove(edited + ".bak")
			// Uninstall removes each file once and keeps the links, the
			// directory and the user's skill.
			if out := e.uninstall(); strings.Contains(out, "edited") {
				t.Errorf("uninstall:\n%s", out)
			}
			e.linksIntact(layout)
			if left := snapshotFiles(t, e.claudeSkills()); len(left) != 2 || string(left["mine/SKILL.md"]) != "mine" || e.read(mine) != "mine" {
				t.Errorf("left in the shared directory: %v", left)
			}
		})
	}
}

// The models.md triggers extend the shared create-task's description too,
// as valid YAML.
func TestSharedSkillsKeepTheTriggers(t *testing.T) {
	e := setup(t)
	e.share("claude→agents")
	os.MkdirAll(e.h.Csquad, 0o755)
	os.WriteFile(filepath.Join(e.h.Csquad, "models.md"), []byte("## When to use create-task\n\nAlso when the user says \"派活\": or #ship it.\n"), 0o644)
	e.install()
	skill := e.read(filepath.Join(e.claudeSkills(), "create-task", "SKILL.md"))
	want := "description: \"Dispatch work to background Claude Code sessions or Codex threads (\\\"tasks\\\" named T1, T2, ...). Use when the user asks to create, publish, queue, dispatch or start a task, or to hand work to background agents. Also when the user says \\\"派活\\\": or #ship it.\"\n"
	if !strings.Contains(skill, "\n"+want+"---\n") || !strings.Contains(skill, "**Codex:** Run every csquad command outside Codex's sandbox") {
		t.Errorf("create-task:\n%s", skill)
	}
}

// fakeOldShared turns an installation into what 3.0.1 left in a shared
// directory: both renderings recorded, the one written last (host) on disk.
func (e *env) fakeOldShared(host string) {
	e.t.Helper()
	claude, codex := e.o.skillFiles("claude"), e.o.skillFiles("codex")
	m := e.manifest()
	m.Files, m.CodexFiles, m.SharedSkills = hashes(claude), hashes(codex), false
	m.AgentsDir = filepath.Join(e.h.Home, ".agents")
	// A file 3.0.1 shipped and this version does not.
	claude["skills/create-task/notes/old.md"], codex["skills/create-task/notes/old.md"] = []byte("old claude"), []byte("old codex")
	m.Files["skills/create-task/notes/old.md"], m.CodexFiles["skills/create-task/notes/old.md"] = sha([]byte("old claude")), sha([]byte("old codex"))
	writeJSON(filepath.Join(e.h.Csquad, "manifest.json"), m)
	files := map[string]map[string][]byte{"claude": claude, "codex": codex}[host]
	for rel, b := range files {
		f := filepath.Join(e.h.Claude, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(f), 0o755)
		os.WriteFile(f, b, 0o644)
	}
}

// Today's state on users' machines: install over it without a word about
// edits, and uninstall it without one.
func TestOldSharedStateIsTakenOverSilently(t *testing.T) {
	for _, layout := range shareLayouts {
		for _, host := range assets.Hosts {
			t.Run(layout+"/"+host+" text", func(t *testing.T) {
				e := setup(t)
				e.share(layout)
				mine := e.userSkill()
				e.install()
				e.fakeOldShared(host)
				if out := e.update(); strings.Contains(out, "edited") || !strings.Contains(out, "is the same directory as") {
					t.Errorf("update:\n%s", out)
				}
				e.noBak(e.claudeSkills())
				e.wantSkills(e.claudeSkills(), assets.Shared)
				if exists(filepath.Join(e.claudeSkills(), "create-task", "notes")) {
					t.Error("the file no longer shipped is left")
				}
				if m := e.manifest(); !m.SharedSkills || len(m.CodexFiles) != 0 || len(m.Files) != len(assets.SkillFiles()) {
					t.Errorf("manifest: %+v", m)
				}

				// Uninstalling the old state directly.
				e.fakeOldShared(host)
				if out := e.uninstall(); strings.Contains(out, "edited") {
					t.Errorf("uninstall:\n%s", out)
				}
				e.noBak(e.claudeSkills())
				e.linksIntact(layout)
				if left := snapshotFiles(t, e.claudeSkills()); len(left) != 2 || e.read(mine) != "mine" {
					t.Errorf("left in the shared directory: %v", left)
				}
			})
		}
	}
}

// The user links two directories csquad filled separately.
func TestSeparateToShared(t *testing.T) {
	for _, layout := range shareLayouts {
		t.Run(layout, func(t *testing.T) {
			e := setup(t)
			os.MkdirAll(filepath.Join(e.h.Home, ".codex"), 0o755)
			e.install()
			e.wantSkills(e.claudeSkills(), "claude")
			e.wantSkills(e.agentsSkills(), "codex")
			// The files of the side the link points to stay, as a user moving
			// them there would leave them.
			kept := e.claudeSkills()
			if layout == "claude→agents" {
				kept = e.agentsSkills()
			}
			saved := files(snapshotFiles(t, kept))
			for _, d := range []string{e.claudeSkills(), e.agentsSkills()} {
				os.RemoveAll(d)
			}
			e.share(layout)
			for rel, b := range saved {
				f := filepath.Join(e.claudeSkills(), rel)
				os.MkdirAll(filepath.Dir(f), 0o755)
				os.WriteFile(f, b, 0o644)
			}
			if out := e.update(); strings.Contains(out, "edited") || !strings.Contains(out, "is the same directory as") {
				t.Errorf("update:\n%s", out)
			}
			e.noBak(e.claudeSkills())
			e.wantSkills(e.claudeSkills(), assets.Shared)
			if m := e.manifest(); !m.SharedSkills || len(m.CodexFiles) != 0 {
				t.Errorf("manifest: %+v", m)
			}
			e.uninstall()
			e.linksIntact(layout)
			if left := snapshotFiles(t, e.claudeSkills()); len(left) != 0 {
				t.Errorf("left: %v", left)
			}
		})
	}
}

// The user removes the link: each side gets its own rendering back.
func TestSharedToSeparate(t *testing.T) {
	for _, layout := range shareLayouts {
		for _, copyFiles := range []bool{false, true} {
			name := layout + "/empty"
			if copyFiles {
				name = layout + "/copied"
			}
			t.Run(name, func(t *testing.T) {
				e := setup(t)
				e.share(layout)
				e.install()
				e.unshare(layout, copyFiles)
				if out := e.update(); strings.Contains(out, "edited") || !strings.Contains(out, "Codex: skills in "+e.agentsSkills()) {
					t.Errorf("update:\n%s", out)
				}
				e.noBak(e.claudeSkills(), e.agentsSkills())
				e.wantSkills(e.claudeSkills(), "claude")
				e.wantSkills(e.agentsSkills(), "codex")
				if m := e.manifest(); m.SharedSkills || len(m.CodexFiles) != len(assets.SkillFiles()) {
					t.Errorf("manifest: %+v", m)
				}
				e.uninstall()
				for _, d := range []string{e.claudeSkills(), e.agentsSkills()} {
					if left := snapshotFiles(t, d); len(left) != 0 {
						t.Errorf("left in %s: %v", d, left)
					}
				}
			})
		}
	}
}

// Uninstall after the link went, without an update: the shared copy left
// on the side that kept it goes too.
func TestUninstallSharedAfterTheLinkWent(t *testing.T) {
	e := setup(t)
	e.share("claude→agents")
	e.install()
	e.unshare("claude→agents", false)
	if out := e.uninstall(); strings.Contains(out, "edited") {
		t.Errorf("uninstall:\n%s", out)
	}
	for _, d := range []string{e.claudeSkills(), e.agentsSkills()} {
		if exists(d) {
			t.Errorf("%s is left: %v", d, snapshotFiles(t, d))
		}
	}
}

// A skills directory linked elsewhere (a junction on Windows) and emptied by
// uninstall stays a link: removing it would delete the link itself.
func TestLinkedSkillsDirStaysOnUninstall(t *testing.T) {
	e := setup(t)
	dotfiles := filepath.Join(e.h.Root, "dotfiles", "skills")
	os.MkdirAll(dotfiles, 0o755)
	os.MkdirAll(e.h.Claude, 0o755)
	testutil.DirLink(t, dotfiles, e.claudeSkills())
	e.install()
	e.uninstall()
	if _, err := os.Lstat(e.claudeSkills()); err != nil || !sameFile(e.claudeSkills(), dotfiles) {
		t.Errorf("the link or its directory went: %v", err)
	}
}

// Codex removed while the directory is shared: the Claude rendering
// replaces the shared one, and nothing of the directory goes.
func TestSharedSkillsWhenCodexGoes(t *testing.T) {
	e := setup(t)
	e.share("claude→agents")
	mine := e.userSkill()
	e.install()
	os.RemoveAll(filepath.Join(e.h.Home, ".codex"))
	if out := e.update(); strings.Contains(out, "edited") || strings.Contains(out, "Codex skills") || strings.Contains(out, "same directory") {
		t.Errorf("update:\n%s", out)
	}
	e.noBak(e.claudeSkills())
	e.wantSkills(e.claudeSkills(), "claude")
	e.linksIntact("claude→agents")
	if m := e.manifest(); m.SharedSkills || len(m.CodexFiles) != 0 || e.read(mine) != "mine" {
		t.Errorf("manifest: %+v", m)
	}
	// Codex back: shared again.
	os.MkdirAll(filepath.Join(e.h.Home, ".codex"), 0o755)
	if out := e.update(); strings.Contains(out, "edited") || !strings.Contains(out, "is the same directory as") {
		t.Errorf("update:\n%s", out)
	}
	e.wantSkills(e.claudeSkills(), assets.Shared)
}
