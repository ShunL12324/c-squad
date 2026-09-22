package squad

import (
	"os"
	"path/filepath"
	"slices"
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

// Delivery stamps every frame with the recipient's current generation, so a rule
// asking the model to compare generations can never fire and only costs it a turn.
// The header itself stays as audit information.
func TestPromptsDoNotDelegateGenerationFiltering(t *testing.T) {
	st := testStore(t)
	s, err := st.read()
	must(t, err)
	for _, id := range []string{"master", "a"} {
		m := s.Members[id]
		m.Role, m.Instructions = "reviewer", "review changes"
		start, err := prompt(s, m, config.Template{Prompt: m.Instructions})
		must(t, err)
		runtime, err := runtimePrompt(s, m)
		must(t, err)
		for _, text := range []string{start, runtime} {
			for _, unwanted := range []string{"Match incoming recipient_generation", "ignore messages for a different generation"} {
				if strings.Contains(text, unwanted) {
					t.Fatalf("%s prompt still delegates generation filtering: %q", id, unwanted)
				}
			}
		}
	}
}

// worker.tmpl forbids plan mode; the runtime must actually enforce it. Denying only
// the entry tool left ExitPlanMode exposed and the invariant on the prompt alone.
func TestWorkerToolDenialCoversPlanMode(t *testing.T) {
	denied := strings.Split(workerDisallowedTools, ",")
	for _, tool := range []string{"AskUserQuestion", "EnterPlanMode", "ExitPlanMode"} {
		if !slices.Contains(denied, tool) {
			t.Fatalf("worker sessions still expose %s", tool)
		}
	}
}
