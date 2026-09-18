package squad

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/config"
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
