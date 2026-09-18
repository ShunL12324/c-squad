package squad

import (
	"database/sql"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CompletionValues reads identifiers without creating a store, refreshing tmux,
// acquiring write transactions, or launching engines. Missing teams yield no values.
func CompletionValues(dir, kind string) ([]string, error) {
	if dir == "" {
		dir = os.Getenv("CSQUAD_STATE_DIR")
	}
	if dir == "" {
		b, _ := os.ReadFile(filepath.Join(currentProjectBase(), "last-team"))
		if len(b) == 0 {
			b, _ = os.ReadFile(filepath.Join(stateBase(), "last-team"))
		}
		dir = strings.TrimSpace(string(b))
	}
	if dir == "" {
		return nil, nil
	}
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
	defer func() { _ = db.Close() }()
	var raw string
	if err = db.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw); err != nil {
		return nil, err
	}
	var state State
	if err = json.Unmarshal([]byte(raw), &state); err != nil {
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
