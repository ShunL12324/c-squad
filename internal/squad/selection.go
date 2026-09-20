package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ResolveTeamDirectory is shared by command execution and read-only completion.
// Explicit names never fall back to another team; a bound session cannot escape
// its team through a selector alias.
func ResolveTeamDirectory(directory, name string) (string, error) {
	if directory != "" && name != "" {
		return "", fmt.Errorf("specify only one team selector")
	}
	dir := directory
	if name != "" {
		var err error
		dir, err = namedTeam(name)
		if err != nil {
			return "", err
		}
	}
	bound := os.Getenv("CSQUAD_STATE_DIR")
	if dir == "" {
		dir = bound
	}
	if dir == "" {
		b, _ := os.ReadFile(filepath.Join(currentProjectBase(), "last-team"))
		if len(b) == 0 {
			b, _ = os.ReadFile(filepath.Join(stateBase(), "last-team"))
		}
		dir = strings.TrimSpace(string(b))
	}
	if bound != "" && cleanPath(dir) != cleanPath(bound) {
		return "", fmt.Errorf("this session is bound to team %q, so it cannot act on team %q; run that command from a terminal outside the team", filepath.Base(cleanPath(bound)), filepath.Base(cleanPath(dir)))
	}
	return dir, nil
}
