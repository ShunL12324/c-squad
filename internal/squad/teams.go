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
func listTeams(o options) error {
	dirs, err := teamDirectories()
	if err != nil {
		return err
	}
	rows := []map[string]any{}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if o["output"] != "json" {
		if _, err = fmt.Fprintln(w, "TEAM\tSTATUS\tMEMBERS\tTASKS\tLOCATION"); err != nil {
			return err
		}
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
		rows = append(rows, map[string]any{"team": s.ID, "status": status, "members": len(s.Members), "tasks": len(s.Tasks), "location": dir})
		if o["output"] != "json" {
			if _, err = fmt.Fprintf(w, "%s\t%s\t%d\t%d\t%s\n", s.ID, status, len(s.Members), len(s.Tasks), dir); err != nil {
				return err
			}
		}
	}
	if o["output"] == "json" {
		return jsonOut(rows)
	}
	return w.Flush()
}

// Never open a local collision for writing. Also retain legacy same-project
// detection so relocating state storage cannot silently duplicate a saved team.
func rejectExistingTeam(root, dir, id string) error {
	if _, err := os.Stat(filepath.Join(dir, "state.db")); err == nil {
		return fmt.Errorf("team %q already exists; use attach or resume", id)
	} else if !os.IsNotExist(err) {
		return err
	}
	legacy := filepath.Join(stateBase(), "teams", id)
	if legacy == dir {
		return nil
	}
	if _, err := os.Stat(filepath.Join(legacy, "state.db")); os.IsNotExist(err) {
		return nil
	} else if err != nil {
		return err
	}
	old, err := readCompletionState(legacy)
	if err != nil {
		return err
	}
	if old.Root == root {
		return fmt.Errorf("team %q already exists; use attach or resume", id)
	}
	return nil
}
