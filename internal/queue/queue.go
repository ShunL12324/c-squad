// Package queue stores queued tasks in one global SQLite database. It records
// only what csquad decided; live session state always comes from Claude Code.
package queue

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// State is the queue-side state of a task.
type State string

// Queue states. A launched task's progress is its Claude Code session state.
const (
	Queued    State = "queued"
	Launched  State = "launched"
	Cancelled State = "cancelled"
	// Failed means the session could not be launched; Error says why.
	Failed State = "failed"
)

// ErrNotFound reports an unknown task ID.
var ErrNotFound = errors.New("task not found")

// Task is one queued unit of work.
type Task struct {
	ID     int64
	Prompt string
	// Name is the session display name the user chose; empty uses DisplayName.
	Name       string
	Cwd        string
	Agent      string
	Model      string
	State      State
	Session    string
	Error      string
	CreatedAt  time.Time
	LaunchedAt time.Time
}

// Label is the short identifier shown to users, e.g. T12.
func (t Task) Label() string { return "T" + strconv.FormatInt(t.ID, 10) }

// DisplayName is the session name used at launch: the chosen name, or the task
// label followed by the prompt's first line.
func (t Task) DisplayName() string {
	if t.Name != "" {
		return t.Name
	}
	return t.Label() + " · " + Summary(t.Prompt, 48)
}

// Summary returns the first non-empty line of text, cut to max runes.
func Summary(text string, max int) string {
	for line := range strings.SplitSeq(text, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			if r := []rune(line); len(r) > max {
				return string(r[:max-1]) + "…"
			}
			return line
		}
	}
	return ""
}

// ParseID accepts "T12", "t12" or "12".
func ParseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimPrefix(s, "T"), "t"), 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid task ID %q; use the form T12", s)
	}
	return id, nil
}

// Store is a handle to the queue database. It is safe for concurrent use by
// several processes: SQLite serialises writers and WAL lets readers proceed.
type Store struct{ db *sql.DB }

// Open opens or creates the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("initialise %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

const schema = `CREATE TABLE IF NOT EXISTS tasks (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	prompt      TEXT NOT NULL,
	name        TEXT NOT NULL DEFAULT '',
	cwd         TEXT NOT NULL,
	agent       TEXT NOT NULL DEFAULT '',
	model       TEXT NOT NULL DEFAULT '',
	state       TEXT NOT NULL,
	session     TEXT NOT NULL DEFAULT '',
	error       TEXT NOT NULL DEFAULT '',
	created_at  INTEGER NOT NULL,
	launched_at INTEGER NOT NULL DEFAULT 0
)`

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Add queues a task and returns it with its ID.
func (s *Store) Add(t Task) (Task, error) {
	t.State = Queued
	t.CreatedAt = time.Now()
	res, err := s.db.Exec(`INSERT INTO tasks (prompt, name, cwd, agent, model, state, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		t.Prompt, t.Name, t.Cwd, t.Agent, t.Model, t.State, t.CreatedAt.UnixMilli())
	if err != nil {
		return t, err
	}
	t.ID, err = res.LastInsertId()
	return t, err
}

// Get returns one task.
func (s *Store) Get(id int64) (Task, error) {
	tasks, err := s.query(`WHERE id = ?`, id)
	if err != nil {
		return Task{}, err
	}
	if len(tasks) == 0 {
		return Task{}, fmt.Errorf("%w: T%d", ErrNotFound, id)
	}
	return tasks[0], nil
}

// List returns tasks in creation order, optionally restricted to one state.
func (s *Store) List(state State) ([]Task, error) {
	if state == "" {
		return s.query(`ORDER BY id`)
	}
	return s.query(`WHERE state = ? ORDER BY id`, state)
}

// Position returns how many queued tasks are ahead of id, counting it, or 0 if
// it is not queued.
func (s *Store) Position(id int64) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM tasks WHERE state = ? AND id <= ? AND EXISTS (SELECT 1 FROM tasks WHERE id = ? AND state = ?)`, Queued, id, id, Queued).Scan(&n)
	return n, err
}

// Cancel removes a queued task from the queue. A task that already launched
// cannot be cancelled here; its session is stopped through Claude Code.
func (s *Store) Cancel(id int64) error {
	res, err := s.db.Exec(`UPDATE tasks SET state = ? WHERE id = ? AND state = ?`, Cancelled, id, Queued)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 {
		return nil
	}
	t, err := s.Get(id)
	if err != nil {
		return err
	}
	if t.State == Launched {
		return fmt.Errorf("%s already launched as session %s; stop it with 'claude stop %s' or from agent view", t.Label(), t.Session, t.Session)
	}
	return fmt.Errorf("%s is %s, not queued", t.Label(), t.State)
}

// Claim atomically moves a queued task out of the queue before it is
// launched, so a cancel that arrives during the launch cannot be lost. It
// reports false when the task is no longer queued.
func (s *Store) Claim(id int64) (bool, error) {
	res, err := s.db.Exec(`UPDATE tasks SET state = ?, launched_at = ? WHERE id = ? AND state = ?`, Launched, time.Now().UnixMilli(), id, Queued)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetSession records the session of a claimed task.
func (s *Store) SetSession(id int64, session string) error {
	_, err := s.db.Exec(`UPDATE tasks SET session = ? WHERE id = ?`, session, id)
	return err
}

// Fail records a launch failure for a claimed task.
func (s *Store) Fail(id int64, cause string) error {
	_, err := s.db.Exec(`UPDATE tasks SET state = ?, error = ? WHERE id = ?`, Failed, cause, id)
	return err
}

func (s *Store) query(clause string, args ...any) ([]Task, error) {
	rows, err := s.db.Query(`SELECT id, prompt, name, cwd, agent, model, state, session, error, created_at, launched_at FROM tasks `+clause, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []Task
	for rows.Next() {
		var t Task
		var created, launched int64
		if err := rows.Scan(&t.ID, &t.Prompt, &t.Name, &t.Cwd, &t.Agent, &t.Model, &t.State, &t.Session, &t.Error, &created, &launched); err != nil {
			return nil, err
		}
		t.CreatedAt = time.UnixMilli(created)
		if launched > 0 {
			t.LaunchedAt = time.UnixMilli(launched)
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
