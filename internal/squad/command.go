package squad

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/ShunL12324/c-squad/internal/process"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func git(cwd string, args ...string) (string, error) { return process.Run(cwd, "git", args...) }

// runGit is the single seam every member Git lookup goes through, so a test can
// count the processes the panel cache actually spawns instead of trusting it.
var runGit = git

func tm(s *State, args ...string) (string, error) {
	return (tmux.Client{Socket: s.Socket}).Run(args...)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func jsonOut(v any) error        { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
