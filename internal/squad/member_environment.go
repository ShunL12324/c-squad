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
	// A symlink loses its binding when an engine filters CSQUAD_* from tool
	// subprocesses. This generation-specific launcher restores identity before
	// entering the native CLI, whose normal flag and stale-generation checks
	// still apply. No user shell configuration or engine inheritance is changed.
	binding := map[string]string{
		"CSQUAD_STATE_DIR":  st.Dir,
		"CSQUAD_MEMBER_ID":  m.ID,
		"CSQUAD_GENERATION": strconv.Itoa(m.Generation),
	}
	var script strings.Builder
	script.WriteString("#!/bin/sh\n")
	for _, key := range []string{"CSQUAD_STATE_DIR", "CSQUAD_MEMBER_ID", "CSQUAD_GENERATION"} {
		value := shellQuote(binding[key])
		fmt.Fprintf(&script, "if [ -n \"${%s-}\" ] && [ \"$%s\" != %s ]; then echo 'csquad: runtime identity conflicts with this session' >&2; exit 1; fi\nexport %s=%s\n", key, key, value, key, value)
	}
	// A plain "not found" from exec would not say what to do.
	fmt.Fprintf(&script, "if [ ! -x %s ]; then echo 'csquad: this team'\\''s csquad build is missing; from a terminal outside the team run: csquad repin %s' >&2; exit 1; fi\n", shellQuote(s.Executable), strings.ReplaceAll(s.ID, "'", ""))
	fmt.Fprintf(&script, "exec %s \"$@\"\n", shellQuote(s.Executable))
	link := filepath.Join(bin, "csquad")
	if current, err := os.ReadFile(link); err != nil || string(current) != script.String() {
		tmp, err := os.CreateTemp(bin, ".launcher-")
		if err != nil {
			return nil, err
		}
		defer func() { _ = os.Remove(tmp.Name()) }()
		if _, err = tmp.WriteString(script.String()); err != nil {
			_ = tmp.Close()
			return nil, err
		}
		if err = tmp.Chmod(0700); err != nil {
			_ = tmp.Close()
			return nil, err
		}
		if err = tmp.Close(); err != nil {
			return nil, err
		}
		if err = os.Rename(tmp.Name(), link); err != nil {
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
