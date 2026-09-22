package squad

import (
	"testing"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

// Test 2: a member's environment has exactly two layers now, the inherited
// selectors and its profile's env. It is materialised onto the member when the
// member is added, so a later change in the caller's environment cannot reach a
// running team.
func TestEnvironmentPrecedenceAndPersistence(t *testing.T) {
	t.Setenv("CODEX_HOME", "/ambient")
	t.Setenv("CLAUDE_CONFIG_DIR", "/inherited")
	env := profileEnv(config.Profile{Engine: config.Codex,
		Env: map[string]string{"CODEX_HOME": "/profile", "EXAMPLE": "a=b c"}})
	// The profile wins where it sets a variable; an inherited selector it leaves
	// alone still reaches the engine.
	if env["CODEX_HOME"] != "/profile" || env["EXAMPLE"] != "a=b c" || env["CLAUDE_CONFIG_DIR"] != "/inherited" {
		t.Fatal(env)
	}
	if inherited := profileEnv(config.Profile{Engine: config.Codex}); inherited["CODEX_HOME"] != "/ambient" {
		t.Fatal(inherited)
	}
	st := testStore(t)
	must(t, st.update(func(s *State) error { s.Members["a"].Env = env; return nil }))
	t.Setenv("CODEX_HOME", "/changed-caller")
	s, e := st.read()
	must(t, e)
	out, e := process.RunEnv("", s.Members["a"].Env, "sh", "-c", `printf '%s|%s' "$CODEX_HOME" "$EXAMPLE"`)
	must(t, e)
	if out != "/profile|a=b c" {
		t.Fatal(out)
	}
	// The keys a profile may not set are rejected when the configuration loads;
	// there is no command-line path that could introduce them later.
	for _, reserved := range []map[string]string{{"TMUX": "bad"}, {"CSQUAD_GENERATION": "9"}, {"BROKEN KEY": ""}} {
		if e := agentenv.Validate(reserved); e == nil {
			t.Fatal("accepted", reserved)
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
