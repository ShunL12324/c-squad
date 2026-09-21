package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEngineCommandOverlayAndSnapshot(t *testing.T) {
	root := t.TempDir()
	user := filepath.Join(root, "config.toml")
	t.Setenv("CSQUAD_CONFIG", user)
	must(t, os.WriteFile(user, []byte("[engine_commands.codex]\nexecutable = '/opt/clients with spaces/codex-alt'\nargs = ['--profile', 'account one']\n[engine_commands.claude]\nexecutable = 'claude-alt'\n"), 0600))
	must(t, os.WriteFile(filepath.Join(root, ".csquad.toml"), []byte("[engine_commands.codex]\nargs = []\n"), 0600))
	c, err := Load(root)
	must(t, err)
	if c.Command(Codex).Executable != "/opt/clients with spaces/codex-alt" || len(c.Command(Codex).Args) != 0 || c.Command(Claude).Executable != "claude-alt" {
		t.Fatalf("partial overlay lost command or did not reset args: %+v", c.EngineCommands)
	}
	b, err := json.Marshal(c)
	must(t, err)
	var snapshot Config
	must(t, json.Unmarshal(b, &snapshot))
	if !reflect.DeepEqual(c.EngineCommands, snapshot.EngineCommands) {
		t.Fatal("snapshot lost engine commands")
	}
	document, err := Document(c)
	must(t, err)
	var roundtrip Config
	must(t, decodeConfig("config.toml", document, &roundtrip))
	if !reflect.DeepEqual(c.Command(Codex), roundtrip.Command(Codex)) || c.Command(Claude).Executable != roundtrip.Command(Claude).Executable {
		t.Fatalf("document lost commands: %s", document)
	}
	if Defaults().Command(Codex).Executable != "codex" || Defaults().Command(Claude).Executable != "claude" {
		t.Fatal("legacy default changed")
	}
}

func TestEngineCommandValidation(t *testing.T) {
	for _, body := range []string{
		`{"engine_commands":{"other":{"executable":"x"}}}`,
		`{"engine_commands":{"codex":{"executable":"./client"}}}`,
		`{"engine_commands":{"codex":{"executable":" "}}}`,
		`{"engine_commands":{"codex":{"executable":"bad\u0000name"}}}`,
		`{"engine_commands":{"claude":{"args":["bad\u0000arg"]}}}`,
		`{"engine_commands":{"codex":{"args":"--profile x"}}}`,
		`{"engine_commands":{"codex":{"argz":[]}}}`,
		`{"engine_commands":{"codex":{"shell":"sh"}}}`,
		`{"engine_commands":{"codex":{"shell":"bash","executable":"cc; echo injected"}}}`,
		`{"engine_commands":{"claude":{"shell":"zsh","executable":"$(touch injected)"}}}`,
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
