package agentenv

import (
	"strings"
	"testing"
)

func TestEmptyOverrideUnsetsEveryVariable(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from-tmux-server")
	t.Setenv("CODEX_HOME", "/inherited-codex")
	t.Setenv("CSQUAD_TEST_EMPTY", "inherited")
	t.Setenv("CSQUAD_TEST_KEPT", "inherited")
	env := Environ(map[string]string{"CLAUDE_CONFIG_DIR": "", "CODEX_HOME": "", "CSQUAD_TEST_EMPTY": "", "CSQUAD_TEST_SET": "value"})
	seen := map[string]string{}
	for _, entry := range env {
		k, v, _ := strings.Cut(entry, "=")
		seen[k] = v
	}
	for _, k := range []string{"CLAUDE_CONFIG_DIR", "CODEX_HOME", "CSQUAD_TEST_EMPTY"} {
		if v, ok := seen[k]; ok {
			t.Fatalf("empty override for %s must unset it, got %q", k, v)
		}
	}
	if seen["CSQUAD_TEST_KEPT"] != "inherited" {
		t.Fatal("a variable without an override must still be inherited")
	}
	if seen["CSQUAD_TEST_SET"] != "value" {
		t.Fatal("a non-empty override was lost")
	}
}
