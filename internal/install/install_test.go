package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/testutil"
)

func TestMain(m *testing.M) {
	testutil.MaybeFakeClaude()
	os.Exit(m.Run())
}

type env struct {
	t        *testing.T
	h        *testutil.Home
	o        Options
	logs     []string
	settings string
}

func setup(t *testing.T) *env {
	h := testutil.NewHome(t)
	src := filepath.Join(h.Root, "csquad-build")
	os.WriteFile(src, []byte("#!/bin/sh\necho binary\n"), 0o755)
	e := &env{t: t, h: h, settings: filepath.Join(h.Claude, "settings.json")}
	e.o = Options{ClaudeDir: h.Claude, Home: h.Csquad, Version: "3.0.0-test", Source: src, Cwd: h.Root, Env: h.Env()}
	e.o.Log = func(s string) { e.logs = append(e.logs, s) }
	return e
}

func (e *env) out() string { s := strings.Join(e.logs, "\n"); e.logs = nil; return s }

func (e *env) install() string {
	e.t.Helper()
	if err := Install(e.o, false); err != nil {
		e.t.Fatalf("install: %v", err)
	}
	return e.out()
}

func (e *env) update() string {
	e.t.Helper()
	if err := Install(e.o, true); err != nil {
		e.t.Fatalf("update: %v", err)
	}
	return e.out()
}

func (e *env) uninstall() string {
	e.t.Helper()
	if err := Uninstall(e.o); err != nil {
		e.t.Fatalf("uninstall: %v", err)
	}
	return e.out()
}

func (e *env) seed(text string) {
	os.MkdirAll(filepath.Dir(e.settings), 0o755)
	os.WriteFile(e.settings, []byte(text), 0o644)
}

func (e *env) read(p string) string {
	b, err := os.ReadFile(p)
	if err != nil {
		e.t.Fatal(err)
	}
	return string(b)
}

func (e *env) command() string {
	return StatuslineCommand(BinaryPath(e.h.Csquad), runtime.GOOS == "windows", e.o.Env.TildeHome())
}

// shell is a path as rendered into commands as an argument.
func shell(p string) string { return assets.ShellPath(p, runtime.GOOS == "windows") }

// binary is the installed binary's path as rendered into skills and commands.
func (e *env) binary() string {
	return assets.ProgramPath(BinaryPath(e.o.Home), runtime.GOOS == "windows", e.o.Env.TildeHome())
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

// snapshot lists every file under dirs with its content.
func snapshot(dirs ...string) []string {
	var out []string
	for _, d := range dirs {
		filepath.Walk(d, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				b, _ := os.ReadFile(p)
				out = append(out, p+"\x00"+string(b))
			} else if err == nil {
				out = append(out, p+"/")
			}
			return nil
		})
	}
	sort.Strings(out)
	return out
}

func TestInstallIntoEmptyDir(t *testing.T) {
	e := setup(t)
	e.install()
	for _, s := range assets.SkillNames() {
		if !exists(filepath.Join(e.h.Claude, "skills", s, "SKILL.md")) {
			t.Errorf("skill %s missing", s)
		}
	}
	want := "{\n  \"statusLine\": {\n    \"type\": \"command\",\n    \"command\": \"" + e.command() + "\",\n    \"refreshInterval\": 10\n  }\n}\n"
	if got := e.read(e.settings); got != want {
		t.Errorf("settings.json:\n%s\nwant\n%s", got, want)
	}
	if got := e.read(filepath.Join(e.h.Csquad, "statusline.json")); !strings.Contains(got, `"present": false`) {
		t.Errorf("statusline.json %s", got)
	}
	// Windows has no executable bit: the .exe name is what makes it one.
	if st, err := os.Stat(BinaryPath(e.h.Csquad)); err != nil || !st.Mode().IsRegular() || runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Errorf("binary not installed executable: %v", err)
	}
	skill := e.read(filepath.Join(e.h.Claude, "skills", "create-task", "SKILL.md"))
	if strings.Contains(skill, "{{CSQUAD}}") || !strings.Contains(skill, e.binary()+" launch --title") {
		t.Errorf("skill not rendered with the binary's path")
	}
	Status(e.o)
	if out := e.out(); !strings.Contains(out, "Status line: wrapped (your original: none)") || !strings.Contains(out, "ok       skills/create-task/SKILL.md") {
		t.Errorf("status:\n%s", out)
	}
}

// A skill is several files: every one is installed (rendered) for both
// hosts, an edited one is kept as .bak, one no longer shipped goes on update,
// and uninstall leaves no skill directory behind.
func TestMultiFileSkills(t *testing.T) {
	e := setup(t)
	os.MkdirAll(filepath.Join(e.h.Home, ".codex"), 0o755)
	e.install()
	ref := filepath.Join(e.h.Claude, "skills", "create-task", "reference.md")
	codexRef := filepath.Join(e.h.Home, ".agents", "skills", "create-task", "reference.md")
	for _, f := range []string{ref, codexRef} {
		if text := e.read(f); strings.Contains(text, "{{CSQUAD}}") || !strings.Contains(text, e.binary()+" retry T12") || strings.Contains(text, "<!--") {
			t.Errorf("%s not rendered for its host:\n%s", f, text)
		}
	}
	Status(e.o)
	if out := e.out(); !strings.Contains(out, "ok       skills/create-task/reference.md") {
		t.Errorf("status:\n%s", out)
	}
	// A file an older version shipped, recorded in the manifest.
	var m Manifest
	readJSON(filepath.Join(e.h.Csquad, "manifest.json"), &m)
	old := filepath.Join(e.h.Claude, "skills", "create-task", "notes", "old.md")
	os.MkdirAll(filepath.Dir(old), 0o755)
	os.WriteFile(old, []byte("old"), 0o644)
	m.Files["skills/create-task/notes/old.md"] = sha([]byte("old"))
	writeJSON(filepath.Join(e.h.Csquad, "manifest.json"), m)
	os.WriteFile(ref, []byte("my notes"), 0o644)
	if out := e.update(); !strings.Contains(out, "skills/create-task/reference.md was edited; saved your version as skills/create-task/reference.md.bak") {
		t.Errorf("update:\n%s", out)
	}
	if exists(old) || exists(filepath.Dir(old)) || e.read(ref+".bak") != "my notes" {
		t.Error("the file no longer shipped is left, or the edit was not kept")
	}
	os.Remove(ref + ".bak")
	e.uninstall()
	for _, d := range []string{filepath.Join(e.h.Claude, "skills", "create-task"), filepath.Join(e.h.Home, ".agents", "skills", "create-task")} {
		if exists(d) {
			t.Errorf("%s is left", d)
		}
	}
}

func TestInstallWrapsTheUsersLineKeepingEverythingElse(t *testing.T) {
	e := setup(t)
	e.seed("{\n    \"theme\": \"dark\",\n    \"statusLine\": {\n        \"type\": \"command\",\n        \"command\": \"echo mine\",\n        \"refreshInterval\": 30\n    },\n    \"env\": {\n        \"A\": \"1\"\n    }\n}\n")
	e.install()
	want := "{\n    \"theme\": \"dark\",\n    \"statusLine\": {\n        \"type\": \"command\",\n        \"command\": \"" + e.command() + "\",\n        \"refreshInterval\": 30\n    },\n    \"env\": {\n        \"A\": \"1\"\n    }\n}\n"
	if got := e.read(e.settings); got != want {
		t.Errorf("settings.json:\n%s", got)
	}
	saved, _ := LoadSaved(e.h.Csquad)
	if saved.CommandLine() != "echo mine" || !exists(filepath.Join(e.h.Csquad, "settings.json.bak")) {
		t.Errorf("saved %+v", saved)
	}
	// Reinstall changes nothing and never wraps twice.
	before := snapshot(e.h.Claude, e.h.Csquad)
	if out := e.install(); strings.Contains(out, "edited") {
		t.Errorf("reinstall: %s", out)
	}
	if after := snapshot(e.h.Claude, e.h.Csquad); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Error("reinstall changed files")
	}
	if saved, _ := LoadSaved(e.h.Csquad); saved.CommandLine() != "echo mine" {
		t.Error("wrapped twice")
	}
}

func TestUpdateBacksUpEditedSkills(t *testing.T) {
	e := setup(t)
	if err := Install(e.o, true); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Errorf("update without install: %v", err)
	}
	e.install()
	skill := filepath.Join(e.h.Claude, "skills", "create-task", "SKILL.md")
	original := e.read(skill)
	os.WriteFile(skill, []byte(original+"\nmy edit\n"), 0o644)
	if out := e.update(); !strings.Contains(out, "skills/create-task/SKILL.md was edited; saved your version as skills/create-task/SKILL.md.bak") {
		t.Errorf("%s", out)
	}
	if e.read(skill) != original || e.read(skill+".bak") != original+"\nmy edit\n" {
		t.Error("backup")
	}
}

func TestUninstallRestoresSettingsByteForByte(t *testing.T) {
	for name, text := range map[string]string{
		"indent 4":  "{\n    \"model\": \"opus\",\n    \"statusLine\": {\n        \"type\": \"command\",\n        \"command\": \"echo mine\",\n        \"refreshInterval\": 2\n    },\n    \"hooks\": {}\n}\n",
		"crlf":      "{\r\n  \"theme\": \"dark\",\r\n  \"statusLine\": {\r\n    \"type\": \"command\",\r\n    \"command\": \"echo mine\"\r\n  }\r\n}\r\n",
		"compact":   `{"theme":"dark","statusLine":{"type":"command","command":"echo mine"}}`,
		"no line":   "{\n  \"theme\": \"dark\"\n}\n",
		"odd space": "{ \"theme\" : \"dark\" ,\n\t\"x\":[1, 2] }",
		"empty":     "{}",
		"blank":     "{ }\n", // only the saved bytes restore this one exactly
	} {
		t.Run(name, func(t *testing.T) {
			e := setup(t)
			e.seed(text)
			e.install()
			wrapped := e.read(e.settings)
			if !strings.Contains(wrapped, e.command()) {
				t.Fatalf("not wrapped: %s", wrapped)
			}
			if strings.Contains(text, "\r\n") && strings.Contains(strings.ReplaceAll(wrapped, "\r\n", ""), "\n") {
				t.Error("a CRLF file got LF line breaks")
			}
			if !strings.Contains(text, "\n") && !strings.HasPrefix(text, "{}") && strings.Contains(wrapped, "\n") {
				t.Error("a compact file got line breaks")
			}
			var v map[string]any
			if err := json.Unmarshal([]byte(wrapped), &v); err != nil {
				t.Fatalf("invalid JSON written: %v\n%s", err, wrapped)
			}
			e.uninstall()
			if got := e.read(e.settings); got != text {
				t.Errorf("restored:\n%q\nwant\n%q", got, text)
			}
			if exists(filepath.Join(e.h.Claude, "skills", "create-task")) || exists(BinaryPath(e.h.Csquad)) {
				t.Error("installed files left")
			}
		})
	}
}

func TestUninstallAfterOtherEditsChangesOnlyTheStatusLine(t *testing.T) {
	e := setup(t)
	e.seed("{\n  \"theme\": \"dark\",\n  \"statusLine\": {\"type\": \"command\", \"command\": \"echo mine\"}\n}\n")
	e.install()
	s := e.read(e.settings)
	s = strings.Replace(s, `"theme": "dark"`, `"theme": "light"`, 1)
	os.WriteFile(e.settings, []byte(s), 0o644)
	e.uninstall()
	if got := e.read(e.settings); got != "{\n  \"theme\": \"light\",\n  \"statusLine\": {\"type\": \"command\", \"command\": \"echo mine\"}\n}\n" {
		t.Errorf("%q", got)
	}
	// Without an original status line, the key goes and the other edit stays.
	e = setup(t)
	e.seed("{\n  \"theme\": \"dark\"\n}\n")
	e.install()
	s = strings.Replace(e.read(e.settings), `"theme": "dark"`, `"theme": "light"`, 1)
	os.WriteFile(e.settings, []byte(s), 0o644)
	e.uninstall()
	if got := e.read(e.settings); got != "{\n  \"theme\": \"light\"\n}\n" {
		t.Errorf("%q", got)
	}
}

func TestUninstallEdgeCases(t *testing.T) {
	// From an empty dir: the settings.json install created goes, and the dirs.
	e := setup(t)
	e.install()
	os.MkdirAll(filepath.Join(e.h.Csquad, "settings"), 0o700)
	os.WriteFile(filepath.Join(e.h.Csquad, "settings", "T3.json"), []byte(`{"env":{"ANTHROPIC_AUTH_TOKEN":"x"}}`), 0o600)
	os.WriteFile(filepath.Join(e.h.Csquad, "csquad.db"), nil, 0o600)
	os.WriteFile(filepath.Join(e.h.Csquad, "models.md"), []byte("# mine\n"), 0o644)
	out := e.uninstall()
	if exists(e.settings) || exists(filepath.Join(e.h.Claude, "skills")) || exists(filepath.Join(e.h.Csquad, "settings")) {
		t.Error("left behind")
	}
	if !exists(filepath.Join(e.h.Csquad, "csquad.db")) || !exists(filepath.Join(e.h.Csquad, "models.md")) || !strings.Contains(out, "it still holds: csquad.db, models.md") {
		t.Errorf("the user's data must stay: %s", out)
	}
	// A status line the user replaced is left alone.
	e = setup(t)
	e.install()
	s := strings.Replace(e.read(e.settings), e.command(), "echo replaced", 1)
	os.WriteFile(e.settings, []byte(s), 0o644)
	if out := e.uninstall(); !strings.Contains(out, "left as it is") || e.read(e.settings) != s {
		t.Errorf("%s", out)
	}
	if err := Uninstall(e.o); err == nil {
		t.Error("uninstall twice")
	}
}

func TestPathsWithSpaces(t *testing.T) {
	e := setup(t)
	e.o.Home = filepath.Join(e.h.Root, "my csquad")
	e.install()
	bin := BinaryPath(e.o.Home)
	quoted := "'" + bin + "'"
	if runtime.GOOS == "windows" {
		quoted = `"` + filepath.ToSlash(bin) + `"`
	}
	if !strings.Contains(e.read(filepath.Join(e.h.Claude, "skills", "task-status", "SKILL.md")), quoted+" status") {
		t.Error("skill path not quoted")
	}
	var s struct{ StatusLine struct{ Command string } }
	if json.Unmarshal([]byte(e.read(e.settings)), &s); s.StatusLine.Command != quoted+" statusline" {
		t.Errorf("wrapper not quoted: %s", s.StatusLine.Command)
	}
	e.uninstall()
	if exists(bin) {
		t.Error("binary left")
	}
}

// A user's home with a space (C:\Users\First Last): on Windows the
// binary is named below ~, which Git Bash and PowerShell both start, where a
// quoted path fails in PowerShell.
func TestHomeWithASpace(t *testing.T) {
	e := setup(t)
	home := filepath.Join(e.h.Root, "First Last")
	e.o.Home = filepath.Join(home, ".csquad")
	e.o.Env = e.h.Env("HOME="+home, "USERPROFILE="+home)
	e.install()
	want := "'" + BinaryPath(e.o.Home) + "'"
	if runtime.GOOS == "windows" {
		want = "~/.csquad/bin/csquad.exe"
	}
	var s struct{ StatusLine struct{ Command string } }
	if json.Unmarshal([]byte(e.read(e.settings)), &s); s.StatusLine.Command != want+" statusline" {
		t.Errorf("status line %q", s.StatusLine.Command)
	}
	if !strings.Contains(e.read(filepath.Join(e.h.Claude, "skills", "task-status", "SKILL.md")), want+" status") {
		t.Error("skill path")
	}
	// Uninstall knows the wrapper it wrote, whatever the environment says now.
	e.o.Env = e.h.Env("HOME="+filepath.Join(e.h.Root, "elsewhere"), "USERPROFILE="+home)
	if out := e.uninstall(); strings.Contains(out, "left as it is") || exists(e.settings) {
		t.Errorf("not restored: %s", out)
	}
}

func TestInvalidSettingsAbortsWithoutChanges(t *testing.T) {
	e := setup(t)
	e.seed("{ nope")
	err := Install(e.o, false)
	if err == nil || !strings.Contains(err.Error(), "nothing was changed") || e.read(e.settings) != "{ nope" || exists(e.h.Csquad) {
		t.Errorf("%v", err)
	}
	// status survives it.
	e.seed("{}")
	e.install()
	e.seed("{ nope")
	Status(e.o)
	if out := e.out(); !strings.Contains(out, "Status line: unknown") {
		t.Errorf("%s", out)
	}
}

func TestWarnings(t *testing.T) {
	e := setup(t)
	project := filepath.Join(e.h.Root, "project")
	os.MkdirAll(filepath.Join(project, ".claude"), 0o755)
	os.WriteFile(filepath.Join(project, ".claude", "settings.json"), []byte(`{"statusLine":{"type":"command","command":"x"}}`), 0o644)
	e.o.Cwd = project
	e.o.Env = e.h.Env("FAKE_VERSION=2.1.258")
	out := e.install()
	if !strings.Contains(out, "overrides the csquad wrapper in this project") || !strings.Contains(out, "Claude Code 2.1.258 is older than 2.1.260") {
		t.Errorf("%s", out)
	}
	e.o.Env = e.h.Env("FAKE_VERSION=2.1.295")
	Status(e.o)
	if out := e.out(); strings.Contains(out, "older than") {
		t.Errorf("%s", out)
	}
}

// csquad 1.x is not migrated: install refuses until 1.x's own uninstall has
// put the user's status line back.
func TestRefusesA1xInstall(t *testing.T) {
	e := setup(t)
	e.seed(`{"statusLine":{"type":"command","command":"node /x/.claude/csquad/tasks.js statusline"}}`)
	before := snapshot(e.h.Claude)
	err := Install(e.o, false)
	if err == nil || !strings.Contains(err.Error(), "run `npx csquad@1 uninstall` first") {
		t.Fatalf("%v", err)
	}
	if strings.Join(snapshot(e.h.Claude), "\n") != strings.Join(before, "\n") || exists(e.h.Csquad) {
		t.Error("something changed")
	}
	e.seed("{}")
	os.MkdirAll(filepath.Join(e.h.Claude, "csquad"), 0o755)
	os.WriteFile(filepath.Join(e.h.Claude, "csquad", "manifest.json"), []byte(`{"version":"1.4.0"}`), 0o644)
	if err := Install(e.o, false); err == nil || !strings.Contains(err.Error(), "csquad 1.x is installed") {
		t.Errorf("%v", err)
	}
}

func TestModelsTriggersExtendTheDescription(t *testing.T) {
	e := setup(t)
	os.MkdirAll(e.h.Csquad, 0o755)
	os.WriteFile(filepath.Join(e.h.Csquad, "models.md"), []byte("# Routing\n\n## When to use create-task\n\nAlso when the user says \"派活\": or #ship it.\nOr \\ backslash.\n\n## Models\n\n- big work: opus\n"), 0o644)
	e.install()
	skill := e.read(filepath.Join(e.h.Claude, "skills", "create-task", "SKILL.md"))
	line := ""
	for _, l := range strings.Split(skill, "\n") {
		if strings.HasPrefix(l, "description: ") {
			line = l
			break
		}
	}
	want := `description: "Dispatch work to background Claude Code sessions or Codex threads (\"tasks\" named T1, T2, ...). Use when the user asks to create, publish, queue, dispatch or start a task, or to hand work to background agents. Also when the user says \"派活\": or #ship it. Or \\ backslash."`
	if line != want {
		t.Errorf("got  %s\nwant %s", line, want)
	}
	if strings.Contains(skill, "big work") {
		t.Error("only the trigger section goes into the description")
	}
	if out := e.update(); strings.Contains(out, "edited") {
		t.Errorf("update after install with models.md: %s", out)
	}
}

// ---- symlinks (dotfile setups): links are never replaced ------------------

func TestSymlinkedSettingsStaysALink(t *testing.T) {
	e := setup(t)
	dots := filepath.Join(e.h.Root, "dotfiles")
	os.MkdirAll(dots, 0o755)
	os.MkdirAll(e.h.Claude, 0o755)
	real := filepath.Join(dots, "settings.json")
	original := "{\n    \"theme\": \"dark\",\n    \"statusLine\": {\"type\": \"command\", \"command\": \"echo mine\"}\n}\n"
	os.WriteFile(real, []byte(original), 0o644)
	testutil.Symlink(t, real, filepath.Join(dots, "hop.json"))
	rel, _ := filepath.Rel(e.h.Claude, filepath.Join(dots, "hop.json"))
	testutil.Symlink(t, rel, e.settings) // relative, chained
	e.install()
	if !isLink(e.settings) || !isLink(filepath.Join(dots, "hop.json")) || !strings.Contains(e.read(real), e.command()) {
		t.Error("links replaced or target not edited")
	}
	e.uninstall()
	if !isLink(e.settings) || e.read(real) != original {
		t.Error("not restored through the links")
	}
	entries, _ := os.ReadDir(dots)
	if len(entries) != 2 {
		t.Errorf("temp files left: %v", entries)
	}
}

func TestDanglingSettingsLink(t *testing.T) {
	e := setup(t)
	dots := filepath.Join(e.h.Root, "dotfiles")
	os.MkdirAll(dots, 0o755)
	os.MkdirAll(e.h.Claude, 0o755)
	testutil.Symlink(t, filepath.Join(dots, "settings.json"), e.settings)
	e.install()
	if !isLink(e.settings) || !strings.Contains(e.read(filepath.Join(dots, "settings.json")), e.command()) {
		t.Error("not written through")
	}
	e.uninstall()
	if !isLink(e.settings) || exists(filepath.Join(dots, "settings.json")) {
		t.Error("the link must stay, dangling again")
	}
	// Into a directory that does not exist: refused, nothing changed.
	e = setup(t)
	os.MkdirAll(e.h.Claude, 0o755)
	target := filepath.Join(e.h.Root, "nowhere", "settings.json")
	testutil.Symlink(t, target, e.settings)
	err := Install(e.o, false)
	if err == nil || !strings.Contains(err.Error(), "symlink") || !strings.Contains(err.Error(), "nothing was changed") || exists(filepath.Join(e.h.Root, "nowhere")) || exists(e.h.Csquad) {
		t.Errorf("%v", err)
	}
}

func TestSymlinkedDirectories(t *testing.T) {
	e := setup(t)
	shared := filepath.Join(e.h.Root, "shared-skills")
	os.MkdirAll(filepath.Join(shared, "their-skill"), 0o755)
	os.WriteFile(filepath.Join(shared, "their-skill", "SKILL.md"), []byte("theirs"), 0o644)
	os.MkdirAll(e.h.Claude, 0o755)
	testutil.Symlink(t, shared, filepath.Join(e.h.Claude, "skills"))
	e.install()
	if !isLink(filepath.Join(e.h.Claude, "skills")) || !exists(filepath.Join(shared, "create-task", "SKILL.md")) {
		t.Error("not written through the skills link")
	}
	e.uninstall()
	entries, _ := os.ReadDir(shared)
	if !isLink(filepath.Join(e.h.Claude, "skills")) || len(entries) != 1 || entries[0].Name() != "their-skill" {
		t.Errorf("the link and other skills must stay: %v", entries)
	}
	// A dangling directory link is refused.
	e = setup(t)
	os.MkdirAll(e.h.Claude, 0o755)
	testutil.Symlink(t, filepath.Join(e.h.Root, "gone"), filepath.Join(e.h.Claude, "skills"))
	if err := Install(e.o, false); err == nil || !strings.Contains(err.Error(), "dangling symlink") || exists(filepath.Join(e.h.Root, "gone")) {
		t.Errorf("%v", err)
	}
	// A symlinked file csquad manages is refused, not written through.
	e = setup(t)
	elsewhere := filepath.Join(e.h.Root, "elsewhere.md")
	os.WriteFile(elsewhere, []byte("mine"), 0o644)
	os.MkdirAll(filepath.Join(e.h.Claude, "skills", "create-task"), 0o755)
	testutil.Symlink(t, elsewhere, filepath.Join(e.h.Claude, "skills", "create-task", "SKILL.md"))
	if err := Install(e.o, false); err == nil || !strings.Contains(err.Error(), "is a symlink") || e.read(elsewhere) != "mine" || exists(e.settings) {
		t.Errorf("%v", err)
	}
}
