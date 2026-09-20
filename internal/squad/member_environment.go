package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// memberEnvironment gives every engine the same short command, even when the
// selected executable has a versioned filename or is absent from ambient PATH.
// Each generation has its own directory so restarting a member does not change
// command resolution for an older process that is still shutting down.
func (st *Store) memberEnvironment(s *State, m *Member) (map[string]string, error) {
	if !filepath.IsAbs(s.Executable) {
		return nil, fmt.Errorf("team executable must be absolute: %q", s.Executable)
	}
	bin, err := filepath.Abs(filepath.Join(st.Dir, "runtime", m.ID, strconv.Itoa(m.Generation), "bin"))
	if err != nil {
		return nil, err
	}
	if strings.ContainsRune(bin, os.PathListSeparator) {
		return nil, fmt.Errorf("member command directory cannot contain PATH separator: %q", bin)
	}
	if err = os.MkdirAll(bin, 0700); err != nil {
		return nil, err
	}
	link := filepath.Join(bin, "csquad")
	if target, _ := os.Readlink(link); target != s.Executable {
		// Rename atomically so concurrent launches never observe a missing link.
		tmp, err := os.MkdirTemp(bin, ".link-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(tmp)
		if err = os.Symlink(s.Executable, filepath.Join(tmp, "csquad")); err != nil {
			return nil, err
		}
		if err = os.Rename(filepath.Join(tmp, "csquad"), link); err != nil {
			return nil, err
		}
	}
	env := agentenv.Merge(m.Env)
	path, overridden := env["PATH"]
	if !overridden {
		path = os.Getenv("PATH")
	}
	parts := []string{bin}
	if path != "" {
		for _, entry := range filepath.SplitList(path) {
			if entry != bin {
				parts = append(parts, entry)
			}
		}
	}
	env["PATH"] = strings.Join(parts, string(os.PathListSeparator))
	return env, nil
}
