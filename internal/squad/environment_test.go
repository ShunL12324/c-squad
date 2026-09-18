package squad

import (
	"testing"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func TestEnvironmentPrecedenceAndPersistence(t *testing.T) {
	t.Setenv("CODEX_HOME", "/ambient")
	_, o := parse([]string{"start", "--env", "CODEX_HOME=/second", "--env=EXAMPLE=a=b c", "--env", "EMPTY="})
	overrides, e := agentenv.Parse(o["env"])
	must(t, e)
	if overrides["EXAMPLE"] != "a=b c" || overrides["CODEX_HOME"] != "/second" {
		t.Fatal(overrides)
	}
	cfg := config.Defaults()
	cfg.Env = map[string]string{"CODEX_HOME": "/config"}
	cfg.StartupEnv = overrides
	env := memberEnv(cfg, config.Template{Env: map[string]string{"CODEX_HOME": "/template"}}, nil)
	if env["CODEX_HOME"] != "/second" {
		t.Fatal(env)
	}
	st := testStore(t)
	must(t, st.update(func(s *State) error { s.Members["a"].Env = env; return nil }))
	t.Setenv("CODEX_HOME", "/changed-caller")
	s, e := st.read()
	must(t, e)
	out, e := process.RunEnv("", s.Members["a"].Env, "sh", "-c", `printf '%s|%s' "$CODEX_HOME" "$EXAMPLE"`)
	must(t, e)
	if out != "/second|a=b c" {
		t.Fatal(out)
	}
	for _, raw := range []string{"BROKEN", "TMUX=bad", "CSQUAD_GENERATION=9"} {
		if _, e := agentenv.Parse(raw); e == nil {
			t.Fatal("accepted", raw)
		}
	}
}
func TestDefaultClaudeHomeIsUnsetInEngine(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "wrong-server-value")
	env := map[string]string{"CLAUDE_CONFIG_DIR": ""}
	out, e := process.RunEnv("", env, "sh", "-c", `if [ "${CLAUDE_CONFIG_DIR+x}" = x ]; then printf set; else printf unset; fi`)
	must(t, e)
	if out != "unset" {
		t.Fatal("default Claude config directory changed", out)
	}
}
