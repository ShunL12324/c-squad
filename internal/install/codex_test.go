package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

func (e *env) agents(name string) string {
	return filepath.Join(e.h.Home, ".agents", "skills", name, "SKILL.md")
}

func TestCodexSkillsOnlyWhenCodexHomeExists(t *testing.T) {
	e := setup(t)
	if out := e.install(); strings.Contains(out, "Codex:") {
		t.Errorf("no ~/.codex: %s", out)
	}
	if exists(filepath.Join(e.h.Home, ".agents")) {
		t.Fatal("~/.agents written without ~/.codex")
	}
	codex := filepath.Join(e.h.Home, ".codex")
	os.MkdirAll(codex, 0o700)
	// The user's Codex files are never touched.
	os.WriteFile(filepath.Join(codex, "config.toml"), []byte("model = \"x\"\n"), 0o600)
	os.WriteFile(filepath.Join(codex, "hooks.json"), []byte(`{"hooks":{}}`), 0o600)
	out := e.update()
	if !strings.Contains(out, "Codex: skills in "+filepath.Join(e.h.Home, ".agents", "skills")) {
		t.Errorf("%s", out)
	}
	for _, name := range []string{"create-task", "finish-task", "message-task", "task-status", "watch-task"} {
		text := e.read(e.agents(name))
		if !strings.Contains(text, shell(BinaryPath(e.h.Csquad))) || !strings.Contains(text, "outside Codex's sandbox") || strings.Contains(text, "<!--") {
			t.Errorf("codex %s:\n%s", name, text)
		}
		if claude := e.read(filepath.Join(e.h.Claude, "skills", name, "SKILL.md")); strings.Contains(claude, "Codex's sandbox") {
			t.Errorf("claude %s carries the Codex note", name)
		}
	}
	if strings.Contains(e.read(e.agents("message-task")), "SendMessage") {
		t.Error("Codex message-task names SendMessage")
	}
	// Only csquad's Stop group is added to hooks.json (codexhooks_test.go).
	if e.read(filepath.Join(codex, "config.toml")) != "model = \"x\"\n" || e.read(filepath.Join(codex, "hooks.json")) != `{"hooks":{"Stop": [`+CodexHookGroup(BinaryPath(e.h.Csquad), e.h.Csquad)+`]}}` {
		t.Error("a Codex file was changed")
	}
	Status(e.o)
	if out := e.out(); !strings.Contains(out, "Codex:       skills in") || !strings.Contains(out, "ok       skills/create-task/SKILL.md") {
		t.Errorf("status: %s", out)
	}
	// An edited skill is kept as .bak on update.
	os.WriteFile(e.agents("task-status"), []byte("mine"), 0o644)
	if out := e.update(); !strings.Contains(out, "skills/task-status/SKILL.md was edited") || e.read(e.agents("task-status")+".bak") != "mine" {
		t.Errorf("%s", out)
	}
	os.Remove(e.agents("task-status") + ".bak")
	// Another tool's skill stays; Codex gone: update takes csquad's back.
	other := filepath.Join(e.h.Home, ".agents", "skills", "other", "SKILL.md")
	os.MkdirAll(filepath.Dir(other), 0o755)
	os.WriteFile(other, []byte("other"), 0o644)
	os.RemoveAll(codex)
	if out := e.update(); !strings.Contains(out, "removed the Codex skills") {
		t.Errorf("%s", out)
	}
	if exists(e.agents("create-task")) || exists(filepath.Dir(e.agents("create-task"))) || !exists(other) {
		t.Error("Codex skills left, or another skill removed")
	}
	if out := e.update(); strings.Contains(out, "Codex:") {
		t.Errorf("second update: %s", out)
	}
}

func TestCodexHomeFromTheEnvironmentAndUninstall(t *testing.T) {
	e := setup(t)
	custom := filepath.Join(e.h.Root, "custom-codex")
	os.MkdirAll(custom, 0o700)
	e.o.Env = e.h.Env("CODEX_HOME=" + custom)
	e.install()
	if !exists(e.agents("create-task")) {
		t.Fatal("not installed for $CODEX_HOME")
	}
	e.uninstall()
	if exists(filepath.Join(e.h.Home, ".agents")) {
		t.Error("uninstall left ~/.agents")
	}
	if !exists(custom) {
		t.Error("uninstall removed $CODEX_HOME")
	}
}

func TestCodexSymlinkedSkillIsRefused(t *testing.T) {
	e := setup(t)
	os.MkdirAll(filepath.Join(e.h.Home, ".codex"), 0o700)
	target := filepath.Join(e.h.Root, "elsewhere.md")
	os.WriteFile(target, []byte("x"), 0o644)
	os.MkdirAll(filepath.Dir(e.agents("create-task")), 0o755)
	testutil.Symlink(t, target, e.agents("create-task"))
	if err := Install(e.o, false); err == nil || !strings.Contains(err.Error(), "is a symlink") {
		t.Fatalf("%v", err)
	}
	if exists(BinaryPath(e.h.Csquad)) {
		t.Error("something was installed")
	}
}
