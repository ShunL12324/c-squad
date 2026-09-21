package squad

import (
	"strings"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/prompts"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func promptData(s *State, m *Member, instructions string) prompts.Data {
	return prompts.Data{
		Team: s.ID, Member: m.ID, Generation: m.Generation, Role: m.Role,
		Instructions: instructions, Engine: string(m.Engine), Cwd: m.Cwd,
		Handoff: m.Handoff, Colors: strings.Join(tmux.ColorNames(), ", "),
	}
}

func prompt(s *State, m *Member, t config.Template) (string, error) {
	return prompts.Render("startup", promptData(s, m, t.Prompt))
}

func runtimePrompt(s *State, m *Member) (string, error) {
	// Resolve legacy role-template instructions exactly as launch does.
	cfg, err := s.effectiveConfig()
	if err != nil {
		return "", err
	}
	instructions := cfg.Templates[m.Role].Prompt
	if m.Instructions != "" || cfg.Engine != "" {
		instructions = m.Instructions
	}
	return prompts.Render("runtime", promptData(s, m, instructions))
}
