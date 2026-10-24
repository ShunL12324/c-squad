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
		got, inserted, err := addCodexHook([]byte(c.in), group)
		if err != nil || string(got) != c.want {
			t.Errorf("%s: %v\n%s\nwant\n%s", c.name, err, got, c.want)
			continue
		}
		if !json.Valid(got) {
			t.Errorf("%s: invalid JSON", c.name)
		}
		// Uninstall restores the bytes exactly (a new file keeps an empty list).
		back, changed := removeCodexHook(got, inserted)
		if !changed {
			t.Errorf("%s: not removed", c.name)
		}
		if c.in != "" && string(back) != c.in && c.name != "empty object" && c.name != "no Stop" && c.name != "empty Stop" {
			t.Errorf("%s: removal gave\n%s", c.name, back)
		}
		if !json.Valid(back) || strings.Contains(string(back), "__csquad__") {
			t.Errorf("%s: after removal %s", c.name, back)
		}
		// Adding again with the same group changes nothing (the trust holds).
		again, ins2, err := addCodexHook(got, group)
		if err != nil || string(again) != string(got) || ins2 != "" {
			t.Errorf("%s: not idempotent: %s", c.name, again)
		}
	}
	// A changed group is replaced in place, keeping its position.
	old := strings.Replace(group, "/b/csquad", "/old/csquad", 1)
	in := `{"hooks": {"Stop": [` + old + `, {"hooks": []}]}}`
	got, _, _ := addCodexHook([]byte(in), group)
	if string(got) != `{"hooks": {"Stop": [`+group+`, {"hooks": []}]}}` {
		t.Errorf("replace: %s", got)
	}
	// Edited around it: every marked group goes, with its separator.
	back, _ := removeCodexHook([]byte(`{"hooks": {"Stop": [`+group+`, {"hooks": []}, `+old+`]}}`), "missing")
	if string(back) != `{"hooks": {"Stop": [{"hooks": []}]}}` {
		t.Errorf("scan removal: %s", back)
	}
	for _, bad := range []string{"{", "[]", `{"hooks": []}`, `{"hooks": {"Stop": {}}}`} {
		if _, _, err := addCodexHook([]byte(bad), group); err == nil {
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
	if !strings.Contains(string(b), `"__csquad__": true`) || !strings.Contains(string(b), "hook --home "+e.h.Csquad+" codex-stop") || !strings.HasPrefix(string(b), "{\n  \"hooks\": {\n    \"Stop\": [\n      {\"hooks\": [{\"type\": \"command\", \"command\": \"say done\"}]},\n      {\"__csquad__\"") {
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
