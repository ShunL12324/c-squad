package squad

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func printConfig(c config.Config) error {
	b, err := config.Document(c)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n%s", config.Path(), b)
	return nil
}

func stateBase() string {
	if d := os.Getenv("CSQUAD_HOME"); d != "" {
		return d
	}
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(d, "csquad")
}

// effectiveConfig keeps running teams on their startup snapshot, including env overrides.
func (s *State) effectiveConfig() (config.Config, error) {
	if s.Config != nil {
		return *s.Config, nil
	}
	return config.Load(s.Root)
}

// engineHelper uses the team's command snapshot for native helper subcommands.
func (s *State) engineHelper(m *Member, args ...string) (string, error) {
	cfg, err := s.effectiveConfig()
	if err != nil {
		return "", err
	}
	command := cfg.Command(m.Engine)
	name, argv, env := command.Invocation(m.Env, args...)
	return process.RunEnv(m.Cwd, env, name, argv...)
}
