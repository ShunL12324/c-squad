package squad

import (
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

func TestDynamicIdentitySurvivesSnapshotWithoutTemplates(t *testing.T) {
	cfg := config.Defaults()
	if len(cfg.Templates) != 0 {
		t.Fatal("defaults still require role templates")
	}
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["a"].Role = "Payments API developer"
		s.Members["a"].Instructions = "Implement the refund endpoint and verify idempotency"
		return nil
	}))
	s, e := st.read()
	must(t, e)
	m := s.Members["a"]
	text := prompt(s, m, config.Template{Prompt: m.Instructions})
	for _, want := range []string{m.Role, m.Instructions, "--role IDENTITY", "--instructions RESPONSIBILITIES"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(text, "--template developer|") {
		t.Fatal("prompt still requires fixed templates")
	}
}
