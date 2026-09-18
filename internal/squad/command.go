package squad

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/ShunL12324/c-squad/internal/process"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

func git(cwd string, args ...string) (string, error) { return process.Run(cwd, "git", args...) }
func tm(s *State, args ...string) (string, error) {
	return (tmux.Client{Socket: s.Socket}).Run(args...)
}
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
func jsonOut(v any) error        { e := json.NewEncoder(os.Stdout); e.SetIndent("", "  "); return e.Encode(v) }
