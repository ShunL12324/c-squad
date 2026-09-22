package squad

import (
	"strings"

	"github.com/ShunL12324/c-squad/internal/prompts"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func promptData(s *State, m *Member, instructions string) prompts.Data {
	return prompts.Data{
		Team: s.ID, Member: m.ID, Generation: m.Generation,
		Instructions: instructions, Engine: string(m.Engine), Cwd: m.Cwd,
		Handoff: m.Handoff, Colors: strings.Join(tmux.ColorNames(), ", "),
	}
}

// A member's responsibilities come only from its own record, written by
// member add --instructions. A profile carries launch settings and nothing a
// prompt reads, so selecting one injects no text here.
func prompt(s *State, m *Member) (string, error) {
	return prompts.Render("startup", promptData(s, m, m.Instructions))
}

func runtimePrompt(s *State, m *Member) (string, error) {
	return prompts.Render("runtime", promptData(s, m, m.Instructions))
}
