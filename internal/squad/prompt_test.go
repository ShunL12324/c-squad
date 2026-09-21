package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/prompts"
)

func TestRecoveryPromptUsesStartupInstructionsAndSharedPolicy(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Engine = ""
		cfg.Templates = map[string]config.Template{"reviewer": {Prompt: "legacy {{.Member}} responsibilities"}}
		s.Config = &cfg
		s.Members["a"].Role = "reviewer"
		s.Members["a"].Handoff = "/tmp/restore.json"
		return nil
	}))
	s, err := st.read()
	must(t, err)
	m := s.Members["a"]
	for _, instructions := range []string{"", "explicit {{.Team}} responsibilities"} {
		m.Instructions = instructions
		tmpl := s.Config.Templates[m.Role]
		if instructions != "" {
			tmpl.Prompt = instructions
		}
		start, err := prompt(s, m, tmpl)
		must(t, err)
		recovered, err := runtimePrompt(s, m)
		must(t, err)
		for _, text := range []string{start, recovered} {
			if !strings.Contains(text, tmpl.Prompt) || !strings.Contains(text, "not message quotas") ||
				!strings.Contains(text, "ordinary uncertainty is not by itself a reason to stop") {
				t.Fatalf("startup/recovery policy drift: %s", text)
			}
		}
	}
}

func TestInvalidRuntimePromptDoesNotStampContextDelivered(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Members["a"].Engine = config.Engine("unsupported")
		return nil
	}))
	err := hookInput(st, "a", 1, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"first"}`))
	if err == nil || !strings.Contains(err.Error(), "prompt requires engine") {
		t.Fatalf("invalid prompt did not fail closed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(st.Dir, "runtime", "a", "messaging-context")); !os.IsNotExist(err) {
		t.Fatalf("failed rendering stamped context delivered: %v", err)
	}
}

func TestUpdatedPolicyReplacesOldRuntimeMarker(t *testing.T) {
	st := testStore(t)
	path := filepath.Join(st.Dir, "runtime", "a", "messaging-context")
	must(t, os.MkdirAll(filepath.Dir(path), 0700))
	must(t, os.WriteFile(path, []byte("3:1:first"), 0600))
	must(t, hookInput(st, "a", 1, strings.NewReader(`{"hook_event_name":"UserPromptSubmit","session_id":"first"}`)))
	marker, err := os.ReadFile(path)
	must(t, err)
	if string(marker) != prompts.Revision+":1:first" {
		t.Fatalf("old policy marker survived: %s", marker)
	}
}
