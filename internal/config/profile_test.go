package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// loadFrom writes a user configuration and loads it, returning the path so a
// test can inspect what the migration wrote back.
func loadFrom(t *testing.T, body string) (Config, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	t.Setenv("CSQUAD_CONFIG", path)
	must(t, os.WriteFile(path, []byte(body), 0600))
	c, err := Load("")
	must(t, err)
	return c, path
}

// Test 15: an empty configuration still launches from code constants rather than
// a second set of configuration fields. Master's built-in model is deliberately
// opus[1m] rather than the opus it used to be; that is a chosen change to the
// zero-configuration default, not a migration losing an explicitly written value.
// Test 14 covers the migration side: master_model = "opus" still migrates to opus.
func TestBuiltinDefaultsWithoutProfiles(t *testing.T) {
	c, _ := loadFrom(t, "")
	worker, name, err := c.ResolveProfile("", false)
	must(t, err)
	if worker.Engine != Codex || worker.Model != "" || name != "" {
		t.Fatalf("worker default changed: %+v %q", worker, name)
	}
	master, name, err := c.ResolveProfile("", true)
	must(t, err)
	if master.Engine != Claude || master.Model != "opus[1m]" || name != "" {
		t.Fatalf("master default changed: %+v %q", master, name)
	}
}

// Test 10 and 11 at the configuration layer: the pointer fields select a profile
// for their role, and an explicit name overrides the pointer. The command-line
// half of both is covered in the squad package.
func TestProfilePointersSelectPerRole(t *testing.T) {
	c, _ := loadFrom(t, `default_profile = "std"
master_profile = "pro"
[profiles.std]
engine = "claude"
model = "sonnet"
[profiles.pro]
engine = "claude"
model = "opus"
`)
	for _, test := range []struct {
		explicit string
		master   bool
		want     string
	}{{"", false, "std"}, {"", true, "pro"}, {"pro", false, "pro"}, {"std", true, "std"}} {
		p, name, err := c.ResolveProfile(test.explicit, test.master)
		must(t, err)
		if name != test.want || p.Model != c.Profiles[test.want].Model {
			t.Fatalf("explicit %q master=%v resolved to %q %+v", test.explicit, test.master, name, p)
		}
	}
}

// Test 13: a profile named master is an ordinary profile. Nothing applies it to
// Master, and nothing applies it to a member whose role happens to say master.
func TestProfileNamesCarryNoBehaviour(t *testing.T) {
	c, _ := loadFrom(t, `[profiles.master]
engine = "codex"
model = "named-like-a-role"
[profiles.developer]
engine = "codex"
model = "also-just-a-name"
`)
	master, name, err := c.ResolveProfile("", true)
	must(t, err)
	if master.Engine != Claude || master.Model != "opus[1m]" || name != "" {
		t.Fatalf("a profile named master was applied to Master: %+v %q", master, name)
	}
	worker, name, err := c.ResolveProfile("", false)
	must(t, err)
	if worker.Engine != Codex || worker.Model != "" || name != "" {
		t.Fatalf("a profile named developer was applied to workers: %+v %q", worker, name)
	}
}

// Test 4 and 12: an unknown name fails with the available names, whether it came
// from the command line or from a pointer field, and a pointer fails at load.
func TestUnknownProfileNamesAreRejected(t *testing.T) {
	c, _ := loadFrom(t, "[profiles.std]\nengine = 'claude'\n[profiles.pro]\nengine = 'claude'\n")
	_, _, err := c.ResolveProfile("typo", false)
	if err == nil || !strings.Contains(err.Error(), "--profile") || !strings.Contains(err.Error(), "available profiles: pro, std") {
		t.Fatalf("unknown --profile: %v", err)
	}
	empty := Config{}
	if _, _, err = empty.ResolveProfile("typo", false); err == nil || !strings.Contains(err.Error(), "no profiles are defined") {
		t.Fatalf("unknown name without any profile: %v", err)
	}
	for _, pointer := range []string{"default_profile", "master_profile"} {
		path := filepath.Join(t.TempDir(), "config.toml")
		t.Setenv("CSQUAD_CONFIG", path)
		must(t, os.WriteFile(path, []byte(pointer+" = 'typo'\n[profiles.std]\nengine = 'claude'\n"), 0600))
		_, err = Load("")
		if err == nil || !strings.Contains(err.Error(), pointer) || !strings.Contains(err.Error(), "available profiles: std") {
			t.Fatalf("%s pointing at a missing profile: %v", pointer, err)
		}
	}
}

// Test 3 at the configuration layer: a profile's command replaces running the
// engine by name for members launched with it, and a profile without one runs
// the engine by name. No member is launched without a profile, so the empty name
// only covers a member recorded before profiles existed.
func TestProfileCommandOverridesAndFallsBack(t *testing.T) {
	c, _ := loadFrom(t, `[profiles.wrapped]
engine = "claude"
[profiles.wrapped.command]
executable = "/usr/bin/profile-claude"
args = ["--wrapped"]
[profiles.plain]
engine = "claude"
`)
	command, warning := c.ProfileCommand("wrapped", Claude)
	if command.Executable != "/usr/bin/profile-claude" || !reflect.DeepEqual(command.Args, []string{"--wrapped"}) || warning != "" {
		t.Fatalf("profile command ignored: %+v %q", command, warning)
	}
	if command, warning = c.ProfileCommand("plain", Claude); command.Executable != "claude" || warning != "" {
		t.Fatalf("profile without a command must run the engine by name: %+v %q", command, warning)
	}
	if command, warning = c.ProfileCommand("", Claude); command.Executable != "claude" || warning != "" {
		t.Fatalf("a member recorded without a profile must run the engine by name: %+v %q", command, warning)
	}
}

// Test 7: a member outlives the profile it was added with, so a deleted profile
// warns and falls back to the built-in default instead of failing the launch.
func TestRemovedProfileFallsBackWithWarning(t *testing.T) {
	c, _ := loadFrom(t, "[profiles.kept]\nengine = 'codex'\n")
	command, warning := c.ProfileCommand("deleted", Codex)
	if command.Executable != "codex" {
		t.Fatalf("removed profile did not fall back: %+v", command)
	}
	if !strings.Contains(warning, "deleted") || !strings.Contains(warning, "built-in default profile") {
		t.Fatalf("removed profile warning: %q", warning)
	}
}

// Test 18 and 19: the shared tables were the layers below a profile's env, so
// they merge into every profile in that order and a profile's own key always
// wins. Afterwards nothing reads them and the file no longer carries them.
func TestLegacySharedEnvironmentMergesIntoProfiles(t *testing.T) {
	c, path := loadFrom(t, `[profiles.own]
engine = "codex"
[profiles.own.env]
CODEX_HOME = "/profile"
LAYER = "profile"
[profiles.bare]
engine = "codex"
[env]
CODEX_HOME = "/shared"
LAYER = "env"
ONLY_SHARED = "yes"
[startup_env]
LAYER = "startup"
ONLY_STARTUP = "yes"
`)
	if len(c.Env) != 0 || len(c.StartupEnv) != 0 {
		t.Fatalf("legacy tables survived migration: %+v %+v", c.Env, c.StartupEnv)
	}
	own := c.Profiles["own"]
	// The old launch path merged [env], then the template's own env, then
	// [startup_env], so a profile's value beats [env] and loses to [startup_env].
	// Getting this backwards would quietly launch a different account.
	if own.Env["CODEX_HOME"] != "/profile" || own.Env["LAYER"] != "startup" {
		t.Fatalf("shared precedence changed: %+v", own.Env)
	}
	if own.Env["ONLY_SHARED"] != "yes" || own.Env["ONLY_STARTUP"] != "yes" {
		t.Fatalf("shared variables did not reach a profile: %+v", own.Env)
	}
	bare := c.Profiles["bare"]
	if bare.Env["LAYER"] != "startup" || bare.Env["CODEX_HOME"] != "/shared" {
		t.Fatalf("shared precedence changed: %+v", bare.Env)
	}
	rewritten, err := os.ReadFile(path)
	must(t, err)
	if strings.Contains(string(rewritten), "[env]") || strings.Contains(string(rewritten), "[startup_env]") {
		t.Fatalf("write-back kept the shared tables: %s", rewritten)
	}
	backup, err := os.ReadFile(path + backupSuffix)
	must(t, err)
	if !strings.Contains(string(backup), "[startup_env]") {
		t.Fatalf("the original file was not preserved: %s", backup)
	}
}

// A file that configures only a shared table defines no profile for it to land
// in, so the built-in defaults are written out first. Losing the variable that
// selects the account would silently launch the wrong one.
func TestLegacySharedEnvironmentReachesTheBuiltinDefaults(t *testing.T) {
	c, _ := loadFrom(t, "[env]\nCODEX_HOME = '/only'\n")
	worker, _, err := c.ResolveProfile("", false)
	must(t, err)
	master, _, err := c.ResolveProfile("", true)
	must(t, err)
	if worker.Env["CODEX_HOME"] != "/only" || master.Env["CODEX_HOME"] != "/only" {
		t.Fatalf("a shared variable was lost: worker %+v master %+v", worker.Env, master.Env)
	}
	if worker.Engine != Codex || master.Engine != Claude || master.Model != "opus[1m]" {
		t.Fatalf("materialised defaults changed what a team launches: %+v %+v", worker, master)
	}
}

// Test 9, plus the rest of the load-time validation: profiles reuse the existing
// rules, so a mistake is reported when the configuration loads rather than when
// a team is already running and someone adds a member.
func TestProfileValidationAtLoad(t *testing.T) {
	for _, test := range []struct{ name, body, want string }{
		{"engine", "[profiles.std]\nengine = 'gpt'\n", "engine"},
		{"managed env", "[profiles.std]\n[profiles.std.env]\nCSQUAD_MEMBER_ID = 'x'\n", "CSQUAD_MEMBER_ID"},
		{"tmux env", "[profiles.std]\n[profiles.std.env]\nTMUX = 'x'\n", "TMUX"},
		{"tmux pane env", "[profiles.std]\n[profiles.std.env]\nTMUX_PANE = 'x'\n", "TMUX_PANE"},
		{"relative executable", "[profiles.std.command]\nexecutable = './wrapper'\n", "profiles.std.command"},
		{"shell", "[profiles.std.command]\nexecutable = 'wrapper'\nshell = 'fish'\n", "profiles.std.command"},
		{"alias name", "[profiles.std.command]\nexecutable = 'two words'\nshell = 'bash'\n", "profiles.std.command"},
		{"name", "[profiles.\"std profile\"]\nengine = 'claude'\n", "letters, digits"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.toml")
			t.Setenv("CSQUAD_CONFIG", path)
			must(t, os.WriteFile(path, []byte(test.body), 0600))
			_, err := Load("")
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("accepted %q: %v", test.body, err)
			}
		})
	}
}

// Test 5: legacy templates become profiles of the same name, their prompt is
// discarded with an explanation, and an explicitly defined profile is never
// overwritten by a template that happens to share its name.
func TestLegacyTemplatesMigrateToProfiles(t *testing.T) {
	c := Config{
		Templates: map[string]Template{
			"reviewer": {Engine: Claude, Model: "opus", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/review"}, Prompt: "review only"},
			"std":      {Engine: Codex, Prompt: "ignored"},
		},
		Profiles: map[string]Profile{"std": {Engine: Claude, Model: "sonnet"}},
	}
	warnings := MigrateLegacy(&c)
	if c.Templates != nil {
		t.Fatal("the legacy table survived migration")
	}
	reviewer := c.Profiles["reviewer"]
	if reviewer.Engine != Claude || reviewer.Model != "opus" || reviewer.Env["CLAUDE_CONFIG_DIR"] != "/review" {
		t.Fatalf("template launch settings lost: %+v", reviewer)
	}
	if std := c.Profiles["std"]; std.Engine != Claude || std.Model != "sonnet" {
		t.Fatalf("an explicit profile was overwritten: %+v", std)
	}
	joined := strings.Join(warnings, "\n")
	for _, want := range []string{"templates.reviewer migrated to profiles.reviewer", "prompt discarded", "--instructions", "templates.std ignored"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("migration warnings missing %q: %s", want, joined)
		}
	}
	// The discarded prompt must not reappear anywhere in the migrated result.
	body, err := Document(c)
	must(t, err)
	if strings.Contains(string(body), "review only") {
		t.Fatal("a template prompt survived into the migrated configuration")
	}
}

// The old launch path merged a template's env below [startup_env], so a variable
// set in both reached the engine with the startup value. The migration has to
// reproduce that order rather than the one the two tables read like.
func TestLegacyStartupEnvironmentOutranksTemplateEnvironment(t *testing.T) {
	c := Config{
		Templates:  map[string]Template{"dev": {Engine: Codex, Env: map[string]string{"CODEX_HOME": "/from-template", "ONLY_TEMPLATE": "yes"}}},
		Env:        map[string]string{"CODEX_HOME": "/from-env", "ONLY_ENV": "yes"},
		StartupEnv: map[string]string{"CODEX_HOME": "/from-startup"},
	}
	MigrateLegacy(&c)
	dev := c.Profiles["dev"]
	if dev.Env["CODEX_HOME"] != "/from-startup" {
		t.Fatalf("startup_env no longer outranks a template's env: %+v", dev.Env)
	}
	if dev.Env["ONLY_TEMPLATE"] != "yes" || dev.Env["ONLY_ENV"] != "yes" {
		t.Fatalf("a variable set in one table only was lost: %+v", dev.Env)
	}
}

// Test 14: legacy top-level engine fields become profiles the pointer fields
// select, the original file is preserved, and loading the rewritten file again
// produces the same configuration.
func TestLegacyEngineFieldsMigrateAndWriteBack(t *testing.T) {
	c, path := loadFrom(t, "engine = 'claude'\nmodel = 'sonnet'\nmaster_engine = 'codex'\nmax_members = 5\n")
	if c.DefaultProfile == "" || c.MasterProfile == "" {
		t.Fatalf("pointers not set: %+v", c)
	}
	if worker := c.Profiles[c.DefaultProfile]; worker.Engine != Claude || worker.Model != "sonnet" {
		t.Fatalf("worker settings changed: %+v", worker)
	}
	if master := c.Profiles[c.MasterProfile]; master.Engine != Codex || master.Model != "" {
		t.Fatalf("master settings changed: %+v", master)
	}
	if c.Engine != "" || c.Model != "" || c.MasterEngine != "" || c.MasterModel != "" {
		t.Fatalf("legacy fields survived: %+v", c)
	}
	backup, err := os.ReadFile(path + backupSuffix)
	must(t, err)
	if !strings.Contains(string(backup), "engine = 'claude'") {
		t.Fatalf("backup is not the original file: %s", backup)
	}
	rewritten, err := os.ReadFile(path)
	must(t, err)
	if hasLegacyFields(path, rewritten) || !strings.Contains(string(rewritten), "default_profile") {
		t.Fatalf("write-back kept legacy fields: %s", rewritten)
	}
	// Idempotent: loading the rewritten file produces the same document and
	// leaves it alone, and the backup taken the first time is never replaced by
	// migrated content.
	again, err := Load("")
	must(t, err)
	document, err := Document(again)
	must(t, err)
	if !reflect.DeepEqual(document, rewritten) {
		t.Fatalf("reload changed the configuration:\nfirst: %s\nagain: %s", rewritten, document)
	}
	if again.DefaultProfile != c.DefaultProfile || again.MasterProfile != c.MasterProfile ||
		again.Profiles[c.DefaultProfile].Model != c.Profiles[c.DefaultProfile].Model {
		t.Fatalf("reload changed the resolved profiles: %+v", again)
	}
	second, err := os.ReadFile(path + backupSuffix)
	must(t, err)
	if !reflect.DeepEqual(second, backup) {
		t.Fatal("the original backup was overwritten by a later migration")
	}
}

// Test 14, migration versus the built-in default: the migration carries over the
// value the user actually wrote. The built-in Master model is opus[1m] now, but a
// configuration that says opus keeps launching opus; the new default only applies
// where nothing was configured.
func TestMigrationKeepsExplicitModelOverBuiltinDefault(t *testing.T) {
	c, _ := loadFrom(t, "master_engine = 'claude'\nmaster_model = 'opus'\n")
	master, _, err := c.ResolveProfile("", true)
	must(t, err)
	if master.Engine != Claude || master.Model != "opus" {
		t.Fatalf("migration rewrote an explicit model: %+v", master)
	}
}

// Test 14, second half: a generated name must never take over a profile the user
// defined, and an equivalent existing profile is reused instead of duplicated.
func TestGeneratedProfileNamesAvoidUserProfiles(t *testing.T) {
	taken := Config{Engine: Claude, Model: "sonnet", Profiles: map[string]Profile{
		"claude-sonnet": {Engine: Codex, Env: map[string]string{"CODEX_HOME": "/mine"}},
	}}
	MigrateLegacy(&taken)
	if kept := taken.Profiles["claude-sonnet"]; kept.Engine != Codex || kept.Env["CODEX_HOME"] != "/mine" {
		t.Fatalf("a user profile was overwritten: %+v", kept)
	}
	generated := taken.Profiles[taken.DefaultProfile]
	if taken.DefaultProfile == "claude-sonnet" || generated.Engine != Claude || generated.Model != "sonnet" {
		t.Fatalf("generated profile %q: %+v", taken.DefaultProfile, generated)
	}
	reused := Config{Engine: Claude, Model: "sonnet", Profiles: map[string]Profile{"std": {Engine: Claude, Model: "sonnet"}}}
	MigrateLegacy(&reused)
	if reused.DefaultProfile != "std" || len(reused.Profiles) != 1 {
		t.Fatalf("an equivalent profile was duplicated: %q %+v", reused.DefaultProfile, reused.Profiles)
	}
}

// Test 5 and 14 together: the legacy templates named developer and master were
// the defaults for their role, so migration keeps pointing at them rather than
// letting the built-in defaults silently change which engine a team starts.
// Nothing matches a profile name against a member at runtime afterwards.
func TestLegacyTemplateDefaultsBecomePointers(t *testing.T) {
	c := Config{Templates: map[string]Template{
		"developer": {Engine: Claude, Model: "sonnet", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/dev"}},
		"master":    {Engine: Claude, Model: "opus"},
	}}
	MigrateLegacy(&c)
	if c.DefaultProfile != "developer" || c.MasterProfile != "master" {
		t.Fatalf("legacy role defaults lost: %q %q", c.DefaultProfile, c.MasterProfile)
	}
	worker, _, err := c.ResolveProfile("", false)
	must(t, err)
	if worker.Engine != Claude || worker.Model != "sonnet" || worker.Env["CLAUDE_CONFIG_DIR"] != "/dev" {
		t.Fatalf("migrated worker default changed: %+v", worker)
	}
	// An explicit top-level field still wins over the inherited template default.
	explicit := Config{Engine: Codex, Templates: map[string]Template{"developer": {Engine: Claude}}}
	MigrateLegacy(&explicit)
	if p, _, _ := explicit.ResolveProfile("", false); p.Engine != Codex {
		t.Fatalf("top-level engine lost to a template: %+v", p)
	}
}

// A JSON user configuration is rewritten as JSON; writing the TOML document to a
// .json path made every later load fail to parse the file.
func TestLegacyJSONConfigurationWritesBackAsJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	t.Setenv("CSQUAD_CONFIG", path)
	must(t, os.WriteFile(path, []byte(`{"engine":"claude","model":"sonnet"}`), 0600))
	first, err := Load("")
	must(t, err)
	rewritten, err := os.ReadFile(path)
	must(t, err)
	if hasLegacyFields(path, rewritten) || !strings.HasPrefix(strings.TrimSpace(string(rewritten)), "{") {
		t.Fatalf("write-back is not migrated JSON: %s", rewritten)
	}
	again, err := Load("")
	must(t, err)
	if again.DefaultProfile != first.DefaultProfile || again.Profiles[again.DefaultProfile].Model != "sonnet" {
		t.Fatalf("reload changed the resolved profiles: %+v", again)
	}
}

// Before profiles every role had a default engine, so a file that set only a
// model was valid. Migration must keep it loadable instead of producing a
// profile with no engine that validation then rejects.
func TestLegacyModelWithoutEngineKeepsTheDefaultEngine(t *testing.T) {
	c, _ := loadFrom(t, "model = 'gpt-5'\nmaster_model = 'sonnet'\n")
	worker, _, err := c.ResolveProfile("", false)
	must(t, err)
	master, _, err := c.ResolveProfile("", true)
	must(t, err)
	if worker.Engine != Codex || worker.Model != "gpt-5" || master.Engine != Claude || master.Model != "sonnet" {
		t.Fatalf("model-only settings migrated to %+v and %+v", worker, master)
	}
}
