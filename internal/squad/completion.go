package squad

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
)

// CompletionValues reads identifiers without creating a store, refreshing tmux,
// acquiring write transactions, or launching engines. Missing teams yield no values.
func CompletionValues(dir, kind string) ([]string, error) {
	if kind == "team" {
		dirs, err := teamDirectories()
		if err != nil {
			return nil, err
		}
		names := []string{}
		for _, dir := range dirs {
			names = append(names, filepath.Base(dir))
		}
		sort.Strings(names)
		return names, nil
	}
	var err error
	dir, err = ResolveTeamDirectory(dir, "")
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, nil
	}
	state, err := readCompletionState(dir)
	if err != nil {
		return nil, err
	}
	values := []string{}
	switch kind {
	case "member":
		for id, m := range state.Members {
			if m.State != MemberStateRemoved {
				values = append(values, id)
			}
		}
	case "task":
		for id := range state.Tasks {
			values = append(values, id)
		}
	case "message":
		for _, m := range state.Messages {
			values = append(values, m.ID)
		}
	case "question":
		for id := range state.Questions {
			values = append(values, id)
		}
	}
	sort.Strings(values)
	return values, nil
}

func readCompletionState(dir string) (*State, error) {
	path, err := filepath.Abs(filepath.Join(dir, "state.db"))
	if err != nil {
		return nil, err
	}
	if _, err = os.Stat(path); err != nil {
		return nil, err
	}
	uri := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(100)"}
	db, err := sql.Open("sqlite", uri.String())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	var raw string
	if err = db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw); err != nil {
		return nil, err
	}
	var state State
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
		return nil, err
	}
	return &state, nil
}
