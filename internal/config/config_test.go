package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func TestTOMLConfigMigrationAndPartialOverlay(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CSQUAD_CONFIG", filepath.Join(dir, "config.toml"))
	must(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_members":12,"templates":{"developer":{"engine":"codex","model":"example"}}}`), 0600))
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, ".csquad.toml"), []byte("[templates.developer.env]\nCODEX_HOME = '/second'\n"), 0600))
	c, e := Load(root)
	must(t, e)
	if c.MaxMembers != 12 || c.Templates["developer"].Engine != Codex || c.Templates["developer"].Model != "example" || c.Templates["developer"].Env["CODEX_HOME"] != "/second" {
		t.Fatalf("unexpected overlay: %+v", c)
	}
	b, e := os.ReadFile(Path())
	must(t, e)
	if !strings.Contains(string(b), "max_members = 12") {
		t.Fatal(string(b))
	}
	if _, e = os.Stat(filepath.Join(dir, "config.json")); e != nil {
		t.Fatal("legacy backup removed")
	}
}

func TestUnknownFieldsAndEngineAreRejected(t *testing.T) {
	for _, test := range []struct{ name, body string }{
		{"config.toml", "max_membrs = 8\n"},
		{"config.toml", "[templates.reviewer]\nengnie = 'claude'\n"},
		{"config.json", `{"max_membrs":8}`},
		{"config.toml", "engine = 'typo'\n"},
	} {
		t.Run(test.body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), test.name)
			t.Setenv("CSQUAD_CONFIG", path)
			must(t, os.WriteFile(path, []byte(test.body), 0600))
			if _, err := Load(""); err == nil {
				t.Fatal("invalid configuration was silently accepted")
			}
		})
	}
}

func TestGeneratedConfigIsDocumentedAndPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("CSQUAD_CONFIG", path)
	got, err := Load("")
	must(t, err)
	if got.Engine != Codex || got.MasterEngine != Claude || got.MaxMembers != 8 {
		t.Fatalf("unexpected generated defaults: %+v", got)
	}
	body, err := os.ReadFile(path)
	must(t, err)
	for _, text := range []string{"# C-Squad", "including Master", "\n[env]\n", "master_model", "do not expand", "--env", "bypass_permissions"} {
		if !strings.Contains(string(body), text) {
			t.Errorf("generated configuration is missing %q", text)
		}
	}
	custom := "# User-maintained comments\nmax_members = 3\n[env]\nEXAMPLE = 'a=b c'\n"
	must(t, os.WriteFile(path, []byte(custom), 0600))
	got, err = Load("")
	must(t, err)
	if got.MaxMembers != 3 || got.Env["EXAMPLE"] != "a=b c" {
		t.Fatalf("custom settings were not preserved: %+v", got)
	}
	body, err = os.ReadFile(path)
	must(t, err)
	if string(body) != custom {
		t.Fatal("loading rewrote the user's configuration")
	}
}

func TestDocumentPreservesEnvironmentAndLegacyValues(t *testing.T) {
	want := Defaults()
	want.Env = map[string]string{"EXAMPLE": "quotes \" and newline\nvalue", "CLAUDE_CONFIG_DIR": ""}
	want.Templates = map[string]Template{"reviewer": {Engine: Claude, Prompt: "review only"}}
	body, err := Document(want)
	must(t, err)
	var got Config
	must(t, decodeConfig("config.toml", body, &got))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("document round trip changed values:\ngot: %#v\nwant: %#v", got, want)
	}
}

func TestMemberSwitchKeysDefaultAndValidate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("CSQUAD_CONFIG", path)
	c, err := Load("")
	must(t, err)
	if c.PreviousKey != "M-Up" || c.NextKey != "M-Down" {
		t.Fatalf("member switch keys did not default to Alt+arrows: %q %q", c.PreviousKey, c.NextKey)
	}
	for _, test := range []struct {
		body string
		ok   bool
	}{
		{"previous_member_key = 'C-M-n'\n", true},
		{"previous_member_key = 'F5'\n", true},
		// An empty value leaves the key to the agent CLI in the engine pane.
		{"previous_member_key = ''\nnext_member_key = ''\n", true},
		{"next_member_key = 'Alt+Down'\n", false},
		{"next_member_key = 'M Down'\n", false},
	} {
		t.Run(test.body, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "config.toml")
			t.Setenv("CSQUAD_CONFIG", file)
			must(t, os.WriteFile(file, []byte(test.body), 0600))
			_, err := Load("")
			if test.ok != (err == nil) {
				t.Fatalf("Load(%q) returned %v", test.body, err)
			}
			if err != nil && !strings.Contains(err.Error(), "tmux key name") {
				t.Fatalf("unhelpful message for a mistyped key: %v", err)
			}
		})
	}
}
