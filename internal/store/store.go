// Package store keeps csquad's ledger: one SQLite table, tasks, in
// $CSQUAD_HOME/csquad.db.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // database/sql driver "sqlite", pure Go
)

// Phases: csquad's own flow. The live session state (working, needs input,
// idle, failed) is always observed from the engine, never stored.
const (
	Waiting   = "waiting"   // after-dependencies not done yet; no session
	Running   = "running"   // session started
	Checking  = "checking"  // worker stopped done; a check is running
	Fixing    = "fixing"    // check failed; findings sent back to the worker
	Merging   = "merging"   // check passed (or none) and merge_into set
	Ready     = "ready"     // done, waiting for the human to accept/finish
	Done      = "done"      // accepted/merged and cleaned up; dependents may start
	NeedsYou  = "needs_you" // stopped for the human
	Failed    = "failed"    // session failed/crashed
	Cancelled = "cancelled" // taken out of the list by cancel; the row keeps its number
)

// Finished reports whether a phase is out of the list for good.
func Finished(phase string) bool { return phase == Done || phase == Cancelled }

// ErrConflict is returned by a compare-and-set that lost.
var ErrConflict = errors.New("the task changed meanwhile")

// ErrNotFound is returned for an id the table does not hold.
var ErrNotFound = errors.New("no such task")

// Task is one row of the tasks table.
type Task struct {
	ID            int
	Title         string
	Cwd           string
	Engine        string
	Model         string
	EngineArgs    []string // extra args after `--` (claude) / thread options (codex)
	SessionID     string   // Claude session id or Codex thread id
	ParentSession string   // the session that dispatched it ("claude:<id>" / "codex:<id>")
	InPlace       bool
	Prompt        string
	WorkerRules   string // the rules appended to the worker's system prompt
	After         []int  // task ids this one waits for
	CheckSpec     string // '' | 'codex[:model]' | 'claude[:model]' | 'cmd:<shell command>'
	MaxRounds     int
	Round         int
	MergeInto     string // '' = do not merge automatically; else the target branch
	Hold          bool   // --no-merge: the user reviews it first; never merged for the tasks queued after it
	Phase         string
	LastCheck     string // JSON {verdict, summary, findings, at}
	Note          string // why the task stopped for the human, last error
	Outcome       string // what the task left (merged branch, final message), set at finish
	Step          string // where the task is inside its phase: merging "asked" (the worker was asked to rebase); needs_you what retry resumes
	CreatedAt     int64  // unix milliseconds
	UpdatedAt     int64  // unix milliseconds; also the compare-and-set version
}

// Label is the task's name in output: T12.
func (t Task) Label() string { return fmt.Sprintf("T%d", t.ID) }

// Name is the session name: "T12 · title".
func (t Task) Name() string { return fmt.Sprintf("T%d · %s", t.ID, t.Title) }

// migrations, applied in order; PRAGMA user_version counts the applied ones.
var migrations = []string{
	`CREATE TABLE tasks (
  id              INTEGER PRIMARY KEY,
  title           TEXT NOT NULL,
  cwd             TEXT NOT NULL,
  engine          TEXT NOT NULL,
  model           TEXT NOT NULL DEFAULT '',
  engine_args     TEXT NOT NULL DEFAULT '[]',
  session_id      TEXT NOT NULL DEFAULT '',
  parent_session  TEXT NOT NULL DEFAULT '',
  in_place        INTEGER NOT NULL DEFAULT 0,
  prompt          TEXT NOT NULL,
  after           TEXT NOT NULL DEFAULT '[]',
  check_spec      TEXT NOT NULL DEFAULT '',
  max_rounds      INTEGER NOT NULL DEFAULT 3,
  round           INTEGER NOT NULL DEFAULT 0,
  merge_into      TEXT NOT NULL DEFAULT '',
  phase           TEXT NOT NULL,
  last_check      TEXT NOT NULL DEFAULT '',
  note            TEXT NOT NULL DEFAULT '',
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  worker_rules    TEXT NOT NULL DEFAULT '',
  outcome         TEXT NOT NULL DEFAULT ''
);
CREATE INDEX tasks_parent ON tasks (parent_session, phase);`,
	`ALTER TABLE tasks ADD COLUMN step TEXT NOT NULL DEFAULT '';`,
	`ALTER TABLE tasks ADD COLUMN hold INTEGER NOT NULL DEFAULT 0;`,
}

// Store is an open database.
type Store struct {
	db  *sql.DB
	Now func() time.Time
}

// Open opens (creating and migrating) the database at path.
func Open(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	// _txlock=immediate: every transaction is BEGIN IMMEDIATE, so a
	// read-then-write (the next id, a compare-and-set) never upgrades a lock.
	dsn := "file:" + filepath.ToSlash(path) + "?_txlock=immediate&_pragma=busy_timeout(15000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Now: time.Now}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > len(migrations) {
		return fmt.Errorf("the database is from a newer csquad (schema %d, this one knows %d)", version, len(migrations))
	}
	for i := version; i < len(migrations); i++ {
		if _, err := tx.Exec(migrations[i]); err != nil {
			return err
		}
	}
	if version < len(migrations) {
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, len(migrations))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

const columns = `id, title, cwd, engine, model, engine_args, session_id, parent_session, in_place, prompt,
  worker_rules, after, check_spec, max_rounds, round, merge_into, phase, last_check, note, outcome, created_at, updated_at, step, hold`

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Task, error) {
	var t Task
	var args, after string
	var inPlace, hold int
	err := r.Scan(&t.ID, &t.Title, &t.Cwd, &t.Engine, &t.Model, &args, &t.SessionID, &t.ParentSession, &inPlace, &t.Prompt,
		&t.WorkerRules, &after, &t.CheckSpec, &t.MaxRounds, &t.Round, &t.MergeInto, &t.Phase, &t.LastCheck, &t.Note, &t.Outcome, &t.CreatedAt, &t.UpdatedAt, &t.Step, &hold)
	if err != nil {
		return t, err
	}
	t.InPlace, t.Hold = inPlace != 0, hold != 0
	_ = json.Unmarshal([]byte(args), &t.EngineArgs)
	_ = json.Unmarshal([]byte(after), &t.After)
	return t, nil
}

func jsonOf(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "[]"
	}
	return string(b)
}

func (s *Store) now() int64 { return s.Now().UnixMilli() }

// Insert adds t with the next id, one above the highest in the table. The
// INSERT itself (in an immediate transaction) is what keeps concurrent
// launches apart; there is no lock file.
func (s *Store) Insert(ctx context.Context, t Task) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return t, err
	}
	defer tx.Rollback()
	var highest int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM tasks`).Scan(&highest); err != nil {
		return t, err
	}
	t.ID = highest + 1
	now := s.now()
	t.CreatedAt, t.UpdatedAt = now, now
	if t.MaxRounds == 0 {
		t.MaxRounds = 3
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tasks (`+columns+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		t.ID, t.Title, t.Cwd, t.Engine, t.Model, jsonOf(t.EngineArgs), t.SessionID, t.ParentSession, b2i(t.InPlace), t.Prompt,
		t.WorkerRules, jsonOf(t.After), t.CheckSpec, t.MaxRounds, t.Round, t.MergeInto, t.Phase, t.LastCheck, t.Note, t.Outcome, t.CreatedAt, t.UpdatedAt, t.Step, b2i(t.Hold))
	if err != nil {
		return t, err
	}
	return t, tx.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// Get returns one task.
func (s *Store) Get(ctx context.Context, id int) (Task, error) {
	t, err := scan(s.db.QueryRowContext(ctx, `SELECT `+columns+` FROM tasks WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Filter selects tasks for List.
type Filter struct {
	Parents  []string // only tasks dispatched by these sessions; nil = every session
	Finished bool     // include done and cancelled tasks
}

// List returns tasks by id.
func (s *Store) List(ctx context.Context, f Filter) ([]Task, error) {
	q := `SELECT ` + columns + ` FROM tasks WHERE 1=1`
	var args []any
	if !f.Finished {
		q += ` AND phase NOT IN (?, ?)`
		args = append(args, Done, Cancelled)
	}
	if f.Parents != nil {
		if len(f.Parents) == 0 {
			return nil, nil
		}
		q += ` AND parent_session IN (` + strings.TrimSuffix(strings.Repeat("?,", len(f.Parents)), ",") + `)`
		for _, p := range f.Parents {
			args = append(args, p)
		}
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// SetSession records the session a task runs in. It does not move
// updated_at: that is the time the task entered its phase.
func (s *Store) SetSession(ctx context.Context, id int, session string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET session_id = ? WHERE id = ?`, session, id)
	return err
}

// SetEngineArgs records a queued task's engine arguments as the engine
// prepared them; like SetSession it does not move updated_at.
func (s *Store) SetEngineArgs(ctx context.Context, id int, args []string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET engine_args = ? WHERE id = ?`, jsonOf(args), id)
	return err
}

// SetHold sets or clears a task's hold (csquad hold, csquad release). Like
// SetSession it does not move updated_at; a compare-and-set from the row as
// it was before loses (Transition compares the hold too).
func (s *Store) SetHold(ctx context.Context, id int, hold bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE tasks SET hold = ? WHERE id = ?`, b2i(hold), id)
	return err
}

// Delete removes a task that never started (a failed launch), so its number
// is free again.
func (s *Store) Delete(ctx context.Context, id int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	return err
}

// Transition writes next over old when the row still holds old's phase,
// updated_at and hold (compare-and-set), inside one immediate transaction:
// every flow field (phase, step, round, max_rounds, last_check, note,
// outcome), never session_id or hold. updated_at moves only when the phase
// or the step changes: it is the time the task entered them. It returns the
// row as stored, or ErrConflict when another writer got there first.
func (s *Store) Transition(ctx context.Context, old, next Task) (Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return next, err
	}
	defer tx.Rollback()
	if next.Phase != old.Phase || next.Step != old.Step {
		next.UpdatedAt = max(s.now(), old.UpdatedAt+1)
	} else {
		next.UpdatedAt = old.UpdatedAt
	}
	res, err := tx.ExecContext(ctx, `UPDATE tasks SET phase = ?, step = ?, round = ?, max_rounds = ?, last_check = ?, note = ?, outcome = ?, updated_at = ?
  WHERE id = ? AND phase = ? AND updated_at = ? AND hold = ?`,
		next.Phase, next.Step, next.Round, next.MaxRounds, next.LastCheck, next.Note, next.Outcome, next.UpdatedAt, old.ID, old.Phase, old.UpdatedAt, b2i(old.Hold))
	if err != nil {
		return next, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return next, ErrConflict
	}
	stored, err := scan(tx.QueryRowContext(ctx, `SELECT `+columns+` FROM tasks WHERE id = ?`, old.ID))
	if err != nil {
		return next, err
	}
	return stored, tx.Commit()
}

// Same reports whether t is still the version of the row cur is: the
// claim a process makes before acting on a task it observed.
func Same(t, cur Task) bool { return t.Phase == cur.Phase && t.UpdatedAt == cur.UpdatedAt }
