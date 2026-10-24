package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const group = `{"__csquad__": true, "hooks": [{"type": "command", "command": "/b/csquad hook --home /h codex-stop", "timeout": 30}]}`

func TestAddCodexHookAsText(t *testing.T) {
	user := `{"type": "command", "command": "notify"}`
	for _, c := range []struct{ name, in, want string }{
		{"no file", "", "{\n  \"hooks\": {\n    \"Stop\": [\n      " + group + "\n    ]\n  }\n}\n"},
		{"no hooks", "{\n  \"x\": 1\n}\n", "{\n  \"x\": 1,\n  \"hooks\": {\"Stop\": [" + group + "]}\n}\n"},
		{"empty object", "{}", `{"hooks": {"Stop": [` + group + `]}}`},
		{"no Stop", "{\n  \"hooks\": {\n    \"PreToolUse\": []\n  }\n}", "{\n  \"hooks\": {\n    \"PreToolUse\": [],\n    \"Stop\": [" + group + "]\n  }\n}"},
		{"empty Stop", `{"hooks": {"Stop": []}}`, `{"hooks": {"Stop": [` + group + `]}}`},
		{"after the user's group", "{\"hooks\": {\"Stop\": [\n  {\"hooks\": [" + user + "]}\n]}}", "{\"hooks\": {\"Stop\": [\n  {\"hooks\": [" + user + "]},\n  " + group + "\n]}}"},
		{"CRLF and tabs kept", "{\r\n\t\"hooks\": {\"Stop\": [\r\n\t\t{\"hooks\": []}\r\n\t]}\r\n}", "{\r\n\t\"hooks\": {\"Stop\": [\r\n\t\t{\"hooks\": []},\r\n\t\t" + group + "\r\n\t]}\r\n}"},
	} {
		got, inserted, err := addCodexHook([]byte(c.in), group, "")
		if err != nil || string(got) != c.want {
			t.Errorf("%s: %v\n%s\nwant\n%s", c.name, err, got, c.want)
			continue
		}
		if !json.Valid(got) || inserted == "" {
			t.Errorf("%s: invalid JSON or nothing recorded (%q)", c.name, inserted)
		}
		// Uninstall restores the bytes exactly; a file csquad created is
		// left empty (and then removed).
		back, changed := removeCodexHook(got, inserted)
		if !changed || string(back) != c.in {
			t.Errorf("%s: removal gave\n%s", c.name, back)
		}
		// Adding again with the same group changes nothing (the trust holds)
		// and keeps the recorded text.
		again, ins2, err := addCodexHook(got, group, inserted)
		if err != nil || string(again) != string(got) || ins2 != inserted {
			t.Errorf("%s: not idempotent: %s (%q)", c.name, again, ins2)
		}
	}
	// A changed group is replaced in place, keeping its position; the
	// recorded text follows it.
	old := strings.Replace(group, "/b/csquad", "/old/csquad", 1)
	in := `{"hooks": {"Stop": [{"hooks": []}, ` + old + `]}}`
	got, text, _ := addCodexHook([]byte(in), group, ", "+old)
	if string(got) != `{"hooks": {"Stop": [{"hooks": []}, `+group+`]}}` || text != ", "+group {
		t.Errorf("replace: %s (%q)", got, text)
	}
	if back, _ := removeCodexHook(got, text); string(back) != `{"hooks": {"Stop": [{"hooks": []}]}}` {
		t.Errorf("removal after replace: %s", back)
	}
	// A group csquad did not record (an older manifest) is replaced too,
	// and nothing is recorded for it.
	if got, text, _ := addCodexHook([]byte(in), group, ""); text != "" || !strings.Contains(string(got), group) {
		t.Errorf("unrecorded: %s (%q)", got, text)
	}
	// Edited around it: every marked group goes, with its separator.
	back, _ := removeCodexHook([]byte(`{"hooks": {"Stop": [`+group+`, {"hooks": []}, `+old+`]}}`), "missing")
	if string(back) != `{"hooks": {"Stop": [{"hooks": []}]}}` {
		t.Errorf("scan removal: %s", back)
	}
	for _, bad := range []string{"{", "[]", `{"hooks": []}`, `{"hooks": {"Stop": {}}}`} {
		if _, _, err := addCodexHook([]byte(bad), group, ""); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestInstallAddsTheCodexHookOnlyWithCodex(t *testing.T) {
	e := setup(t)
	codex := filepath.Join(e.h.Home, ".codex")
	e.install()
	if _, err := os.Stat(filepath.Join(codex, "hooks.json")); err == nil {
		t.Fatal("hooks.json written without Codex")
	}
	os.MkdirAll(codex, 0o755)
	file := filepath.Join(codex, "hooks.json")
	mine := "{\n  \"hooks\": {\n    \"Stop\": [\n      {\"hooks\": [{\"type\": \"command\", \"command\": \"say done\"}]}\n    ]\n  }\n}\n"
	os.WriteFile(file, []byte(mine), 0o644)
	out := e.update()
	if !strings.Contains(out, "approve it once in Codex with /hooks") {
		t.Errorf("%s", out)
	}
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), `"__csquad__": true`) || !strings.Contains(string(b), "hook --home "+shell(e.h.Csquad)+" codex-stop") || !strings.HasPrefix(string(b), "{\n  \"hooks\": {\n    \"Stop\": [\n      {\"hooks\": [{\"type\": \"command\", \"command\": \"say done\"}]},\n      {\"__csquad__\"") {
		t.Errorf("hooks.json:\n%s", b)
	}
	if out := e.update(); strings.Contains(out, "Codex: Stop hook") {
		t.Errorf("an unchanged group is not rewritten: %s", out)
	}
	e.uninstall()
	if b, _ := os.ReadFile(file); string(b) != mine {
		t.Errorf("uninstall:\n%s", b)
	}
}

func TestCodexHooksFileCsquadCreatedGoesOnUninstall(t *testing.T) {
	e := setup(t)
	os.MkdirAll(filepath.Join(e.h.Home, ".codex"), 0o755)
	e.install()
	file := filepath.Join(e.h.Home, ".codex", "hooks.json")
	b, _ := os.ReadFile(file)
	var m Manifest
	if !readJSON(filepath.Join(e.h.Csquad, "manifest.json"), &m) || m.HooksText == "" || m.HooksText != string(b) {
		t.Errorf("hooksText %q, file %q", m.HooksText, b)
	}
	e.update()
	if readJSON(filepath.Join(e.h.Csquad, "manifest.json"), &m); m.HooksText != string(b) {
		t.Errorf("update lost hooksText: %q", m.HooksText)
	}
	e.uninstall()
	if exists(file) {
		t.Error("the hooks.json csquad created is left")
	}
}
