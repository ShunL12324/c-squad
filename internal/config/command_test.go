package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// Test 20: a legacy [engine_commands.ENGINE] table launched every member using
// that engine, so it is merged into the profiles that launch it, a profile with
// its own command is left alone, and the partial project overlay still applies
// before the merge. Nothing keeps reading the legacy table afterwards.
func TestLegacyEngineCommandsMigrateIntoProfiles(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "config.toml")
	t.Setenv("CSQUAD_CONFIG", user)
	must(t, os.WriteFile(user, []byte(`[engine_commands.codex]
executable = "/opt/clients with spaces/codex-alt"
args = ["--profile", "account one"]
[engine_commands.claude]
executable = "/usr/bin/claude-alt"
[profiles.plain]
engine = "codex"
[profiles.wrapped]
engine = "codex"
[profiles.wrapped.command]
executable = "/usr/bin/own-codex"
`), 0600))
	must(t, os.WriteFile(filepath.Join(root, ".csquad.toml"), []byte("[engine_commands.codex]\nargs = []\n"), 0600))
	c, err := Load(root)
	must(t, err)
	if len(c.EngineCommands) != 0 {
		t.Fatalf("the legacy table survived migration: %+v", c.EngineCommands)
	}
	plain := c.Profiles["plain"].LaunchCommand(Codex)
	if plain.Executable != "/opt/clients with spaces/codex-alt" || len(plain.Args) != 0 {
		t.Fatalf("partial overlay lost the command or did not reset args: %+v", plain)
	}
	if wrapped := c.Profiles["wrapped"].LaunchCommand(Codex); wrapped.Executable != "/usr/bin/own-codex" {
		t.Fatalf("a profile's own command was overwritten: %+v", wrapped)
	}
	// The claude table had no profile of its own, so the migration writes the
	// built-in master profile out and merges into that.
	master, _, err := c.ResolveProfile("", true)
	must(t, err)
	if master.LaunchCommand(Claude).Executable != "/usr/bin/claude-alt" {
		t.Fatalf("master lost its legacy launcher: %+v", master)
	}
	// A rewritten file reloads into the same configuration, and the launchers are
	// still resolved from the profiles rather than the deleted table.
	again, err := Load(root)
	must(t, err)
	for name, want := range c.Profiles {
		got := again.Profiles[name]
		if got.Engine != want.Engine || got.LaunchCommand(got.Engine).Executable != want.LaunchCommand(want.Engine).Executable {
			t.Fatalf("reload changed profiles.%s: %+v want %+v", name, got, want)
		}
	}
}

// An engine_commands entry for an engine no profile launches gets a profile of
// its own: the engine used to be chosen per member, so a saved team can hold
// members that launch it. An entry that names no engine at all is dropped, with
// a warning, rather than written out as a profile that cannot validate.
func TestLegacyEngineCommandsWithoutAProfileAreKept(t *testing.T) {
	c := Config{Profiles: map[string]Profile{"only": {Engine: Codex}}, DefaultProfile: "only", MasterProfile: "only",
		EngineCommands: map[Engine]Command{Claude: {Executable: "/usr/bin/wrapper"}, "other": {Executable: "/usr/bin/x"}}}
	joined := strings.Join(MigrateLegacy(&c), "\n")
	command, warning := c.ProfileCommand(c.LaunchProfileFor(Claude, false), Claude)
	if command.Executable != "/usr/bin/wrapper" || warning != "" {
		t.Fatalf("a launcher was lost because no profile used its engine: %+v %q", command, warning)
	}
	if !strings.Contains(joined, "engine_commands.other dropped") {
		t.Fatalf("missing warning for an entry naming no engine: %s", joined)
	}
	must(t, c.ValidateProfiles())
}

// Command validation is unchanged; it now reaches the same fields through the
// profile the legacy table was merged into.
func TestEngineCommandValidation(t *testing.T) {
	for _, body := range []string{
		`{"engine_commands":{"codex":{"executable":"./client"}}}`,
		`{"engine_commands":{"codex":{"executable":" "}}}`,
		"{\"engine_commands\":{\"codex\":{\"executable\":\"bad\\u0000name\"}}}",
		"{\"engine_commands\":{\"claude\":{\"args\":[\"bad\\u0000arg\"]}}}",
		`{"engine_commands":{"codex":{"args":"--profile x"}}}`,
		`{"engine_commands":{"codex":{"argz":[]}}}`,
		`{"engine_commands":{"codex":{"shell":"sh"}}}`,
		`{"engine_commands":{"codex":{"shell":"bash","executable":"cc; echo injected"}}}`,
		`{"engine_commands":{"claude":{"shell":"zsh","executable":"$(touch injected)"}}}`,
		`{"profiles":{"std":{"engine":"codex","command":{"executable":"./client"}}}}`,
	} {
		t.Run(body, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.json")
			t.Setenv("CSQUAD_CONFIG", path)
			must(t, os.WriteFile(path, []byte(body), 0600))
			if _, err := Load(""); err == nil {
				t.Fatal("accepted invalid command configuration")
			}
		})
	}
	command := Command{Args: []string{"", "$(touch never)", "a b"}}
	args := command.Arguments("resume", "thread")
	if !reflect.DeepEqual(args, []string{"", "$(touch never)", "a b", "resume", "thread"}) {
		t.Fatal(args)
	}
	args[0] = "changed"
	if command.Args[0] != "" {
		t.Fatal("argument composition modified snapshot")
	}
}
