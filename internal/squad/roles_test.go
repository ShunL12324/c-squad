package squad

import (
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

func TestDynamicIdentitySurvivesSnapshotWithoutTemplates(t *testing.T) {
	cfg := config.Defaults()
	if len(cfg.Templates) != 0 || len(cfg.Profiles) != 0 {
		t.Fatal("defaults still ship canned identities")
	}
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["a"].Instructions = "Implement the refund endpoint and verify idempotency"
		return nil
	}))
	s, e := st.read()
	must(t, e)
	m := s.Members["a"]
	text, promptErr := prompt(s, m)
	must(t, promptErr)
	if !strings.Contains(text, m.Instructions) {
		t.Fatalf("missing %q", m.Instructions)
	}
	// Responsibilities are the only identity a member carries, and Master's
	// prompt documents the single flag that supplies them.
	master, promptErr := prompt(s, s.Members["master"])
	must(t, promptErr)
	if !strings.Contains(master, "--instructions RESPONSIBILITIES") {
		t.Fatal("master prompt lost the recruiting flag")
	}
	text += master
	for _, gone := range []string{"--template developer|", "--role IDENTITY"} {
		if strings.Contains(text, gone) {
			t.Fatalf("prompt still advertises %q", gone)
		}
	}
}
