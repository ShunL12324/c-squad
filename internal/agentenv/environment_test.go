package agentenv

import (
	"strings"
	"testing"
)

func TestEngineUnsetDoesNotEraseOtherEmptyValues(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/from-tmux-server")
	t.Setenv("CSQUAD_TEST_EMPTY", "inherited")
	env := Environ(map[string]string{"CLAUDE_CONFIG_DIR": "", "CSQUAD_TEST_EMPTY": ""})
	foundEmpty := false
	for _, entry := range env {
		if strings.HasPrefix(entry, "CLAUDE_CONFIG_DIR=") {
			t.Fatal("default Claude directory must remain unset")
		}
		if entry == "CSQUAD_TEST_EMPTY=" {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Fatal("ordinary empty override was lost")
	}
}
