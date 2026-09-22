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
	path := filepath.Join(dir, "config.toml")
	t.Setenv("CSQUAD_CONFIG", path)
	must(t, os.WriteFile(path, []byte("max_members = 12\n[templates.developer]\nengine = 'codex'\nmodel = 'example'\n"), 0600))
	root := t.TempDir()
	must(t, os.WriteFile(filepath.Join(root, ".csquad.toml"), []byte("[templates.developer.env]\nCODEX_HOME = '/second'\n"), 0600))
	c, e := Load(root)
	must(t, e)
	// The overlaid template is merged field by field and then migrated to a
	// profile of the same name, which default_profile inherits from it.
	if c.MaxMembers != 12 || c.Profiles["developer"].Engine != Codex || c.Profiles["developer"].Model != "example" || c.Profiles["developer"].Env["CODEX_HOME"] != "/second" || c.DefaultProfile != "developer" {
		t.Fatalf("unexpected overlay: %+v", c)
	}
	// The rewritten user file keeps the user's own settings and only the value a
	// project overlay supplied stays out of it.
	b, e := os.ReadFile(path)
	must(t, e)
	if !strings.Contains(string(b), "max_members = 12") || strings.Contains(string(b), "/second") {
		t.Fatal(string(b))
	}
}

// A legacy JSON configuration is imported once into the new TOML file, migrated
// on the way in so the new file launches what the old one launched, and kept on
// disk as a backup.
func TestLegacyJSONConfigurationIsImportedAndMigrated(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CSQUAD_CONFIG", filepath.Join(dir, "config.toml"))
	must(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"max_members":12,"templates":{"developer":{"engine":"codex","model":"example"}}}`), 0600))
	c, e := Load("")
	must(t, e)
	if c.MaxMembers != 12 || c.DefaultProfile != "developer" || c.Profiles["developer"].Model != "example" {
		t.Fatalf("legacy import changed what a team launches: %+v", c)
	}
	// Master had no legacy setting, so it takes the built-in profile, written out
	// beside the imported one.
	if c.MasterProfile != "claude-opus" || c.Profiles["claude-opus"].Model != "opus[1m]" {
		t.Fatalf("built-in master profile not seeded: %+v", c)
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

// Test 22: a new configuration is written with the built-in profiles spelled out
// and both pointers set, so the defaults are visible and editable rather than
// hidden in code, and a file that already uses profiles is never rewritten.
func TestGeneratedConfigIsDocumentedAndPreserved(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("CSQUAD_CONFIG", path)
	got, err := Load("")
	must(t, err)
	if got.MaxMembers != 8 || got.DefaultProfile != "codex" || got.MasterProfile != "claude-opus" {
		t.Fatalf("unexpected generated defaults: %+v", got)
	}
	if worker := got.Profiles["codex"]; worker.Engine != Codex || worker.Model != "" {
		t.Fatalf("generated worker profile: %+v", worker)
	}
	if master := got.Profiles["claude-opus"]; master.Engine != Claude || master.Model != "opus[1m]" {
		t.Fatalf("generated master profile: %+v", master)
	}
	body, err := os.ReadFile(path)
	must(t, err)
	for _, text := range []string{"# C-Squad", "including Master", "[profiles.codex]", "[profiles.claude-opus]",
		`default_profile = 'codex'`, `master_profile = 'claude-opus'`, `model = 'opus[1m]'`, "do not expand", "bypass_permissions"} {
		if !strings.Contains(string(body), text) {
			t.Errorf("generated configuration is missing %q", text)
		}
	}
	// Legacy tables are not written into a new file; only a file that still has
	// one is migrated, and that is covered by the migration tests.
	for _, text := range []string{"\n[env]\n", "\n[startup_env]\n", "\n[engine_commands", "--env"} {
		if strings.Contains(string(body), text) {
			t.Errorf("generated configuration still offers %q", text)
		}
	}
	custom := "# User-maintained comments\nmax_members = 3\n[profiles.mine]\nengine = 'claude'\n[profiles.mine.env]\nEXAMPLE = 'a=b c'\n"
	must(t, os.WriteFile(path, []byte(custom), 0600))
	got, err = Load("")
	must(t, err)
	if got.MaxMembers != 3 || got.Profiles["mine"].Env["EXAMPLE"] != "a=b c" {
		t.Fatalf("custom settings were not preserved: %+v", got)
	}
	body, err = os.ReadFile(path)
	must(t, err)
	if string(body) != custom {
		t.Fatal("loading rewrote a configuration that had nothing to migrate")
	}
}

func TestDocumentPreservesEnvironmentAndLegacyValues(t *testing.T) {
	want := Defaults()
	want.Profiles = map[string]Profile{"std": {Engine: Claude,
		Env: map[string]string{"EXAMPLE": "quotes \" and newline\nvalue", "CLAUDE_CONFIG_DIR": ""}}}
	want.DefaultProfile = "std"
	// A legacy table still round trips: a file being migrated is decoded, and the
	// write-back is generated from the same document writer.
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
