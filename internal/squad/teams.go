package squad

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"text/tabwriter"
)

func namedTeam(name string) (string, error) {
	if !validID.MatchString(name) {
		return "", fmt.Errorf("invalid team name %q", name)
	}
	for _, base := range []string{currentProjectBase(), stateBase()} {
		dir := filepath.Join(base, "teams", name)
		if _, err := os.Stat(filepath.Join(dir, "state.db")); err == nil {
			return dir, nil
		}
	}
	return "", fmt.Errorf("team %q not found; run csquad list", name)
}
func teamDirectories() ([]string, error) {
	dirs := []string{}
	seen := map[string]bool{}
	for _, base := range []string{currentProjectBase(), stateBase()} {
		entries, err := os.ReadDir(filepath.Join(base, "teams"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			dir := filepath.Join(base, "teams", entry.Name())
			if seen[dir] {
				continue
			}
			if _, err = os.Stat(filepath.Join(dir, "state.db")); err != nil {
				continue
			}
			seen[dir] = true
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}
func listTeams() error {
	dirs, err := teamDirectories()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if _, err = fmt.Fprintln(w, "TEAM\tSTATUS\tMEMBERS\tTASKS\tLOCATION"); err != nil {
		return err
	}
	for _, dir := range dirs {
		st, e := openStore(dir)
		if e != nil {
			return e
		}
		s, e := st.read()
		_ = st.DB.Close()
		if e != nil {
			return e
		}
		status := "stopped"
		if s.Active {
			status = "running"
			if masterGone(s) {
				status = "interrupted"
			}
		}
		if _, err = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", s.ID, status, len(s.Members), len(s.Tasks), dir); err != nil {
			return err
		}
	}
	return w.Flush()
}

func startExisting(dir string, o options) error {
	st, err := openStore(dir)
	if err != nil {
		return err
	}
	defer func() { _ = st.DB.Close() }()
	s, err := st.read()
	if err != nil {
		return err
	}
	if s.Active && !masterGone(s) {
		if o["detach"] == "true" {
			return jsonOut(map[string]string{"team": dir, "state": "running"})
		}
		return attach(st, "master")
	}
	return resumeTeam(st, o)
}
