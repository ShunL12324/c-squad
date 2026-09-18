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
		s.Members["a"].Role = "支付接口负责人"
		s.Members["a"].Instructions = "实现退款接口并验证幂等性"
		return nil
	}))
	s, e := st.read()
	must(t, e)
	m := s.Members["a"]
	text := prompt(s, m, st, config.Template{Prompt: m.Instructions})
	for _, want := range []string{m.Role, m.Instructions, "--role IDENTITY", "--instructions RESPONSIBILITIES"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q", want)
		}
	}
	if strings.Contains(text, "--template developer|") {
		t.Fatal("prompt still requires fixed templates")
	}
}
