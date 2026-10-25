package store

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sort"
	"sync"
	"testing"
)

func open(t *testing.T, path string) *Store {
	t.Helper()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestInsertNumbersAndRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "csquad.db"))
	a, err := s.Insert(ctx, Task{Title: "a", Cwd: "/x", Engine: "claude", Prompt: "p", Phase: Running, EngineArgs: []string{"--effort", "high"}, InPlace: true, ParentSession: "claude:s1"})
	if err != nil || a.ID != 1 || a.MaxRounds != 3 {
		t.Fatalf("%+v %v", a, err)
	}
	b, _ := s.Insert(ctx, Task{Title: "b", Cwd: "/x", Engine: "claude", Prompt: "p", Phase: Running})
	if b.ID != 2 {
		t.Errorf("second id %d", b.ID)
	}
	got, err := s.Get(ctx, 1)
	if err != nil || got.Title != "a" || !got.InPlace || len(got.EngineArgs) != 2 || got.ParentSession != "claude:s1" || len(got.After) != 0 {
		t.Errorf("%+v %v", got, err)
	}
	if _, err := s.Get(ctx, 9); !errors.Is(err, ErrNotFound) {
		t.Errorf("%v", err)
	}
	if list, _ := s.List(ctx, Filter{Parents: []string{"claude:s1"}}); len(list) != 1 || list[0].ID != 1 {
		t.Errorf("parents filter %+v", list)
	}
	if list, _ := s.List(ctx, Filter{Parents: []string{}}); len(list) != 0 {
		t.Error("an empty parents list matches nothing")
	}
	// A deleted (never started) task frees its number; a finished one does not.
	s.Delete(ctx, 2)
	if c, _ := s.Insert(ctx, Task{Title: "c", Cwd: "/x", Engine: "claude", Prompt: "p", Phase: Running}); c.ID != 2 {
		t.Errorf("after delete %d", c.ID)
	}
}

// Concurrent launches (separate processes in real use: separate handles
// here) never get the same number.
func TestConcurrentInserts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "csquad.db")
	open(t, path)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var ids []int
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s := open(t, path)
			for j := 0; j < 5; j++ {
				task, err := s.Insert(context.Background(), Task{Title: "x", Cwd: "/", Engine: "claude", Prompt: "p", Phase: Running})
				if err != nil {
					t.Error(err)
					return
				}
				mu.Lock()
				ids = append(ids, task.ID)
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	sort.Ints(ids)
	for i, id := range ids {
		if id != i+1 {
			t.Fatalf("ids %v", ids)
		}
	}
}

func TestTransitionIsCompareAndSet(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "csquad.db"))
	t1, _ := s.Insert(ctx, Task{Title: "a", Cwd: "/", Engine: "claude", Prompt: "p", Phase: Running})
	next := t1
	next.Phase = Done
	stored, err := s.Transition(ctx, t1, next)
	if err != nil || stored.Phase != Done || stored.UpdatedAt <= t1.UpdatedAt {
		t.Fatalf("%+v %v", stored, err)
	}
	// A second writer with the old snapshot loses.
	other := t1
	other.Phase = Cancelled
	if _, err := s.Transition(ctx, t1, other); !errors.Is(err, ErrConflict) {
		t.Errorf("%v", err)
	}
	if list, _ := s.List(ctx, Filter{}); len(list) != 0 {
		t.Error("done tasks are not listed")
	}
	if list, _ := s.List(ctx, Filter{Finished: true}); len(list) != 1 {
		t.Error("Finished lists them")
	}
	// Recording a session does not move the version.
	s.SetSession(ctx, 1, "abc")
	if got, _ := s.Get(ctx, 1); got.UpdatedAt != stored.UpdatedAt || got.SessionID != "abc" {
		t.Errorf("%+v", got)
	}
}

func TestSchemaHasEveryColumnAndRefusesNewer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "csquad.db")
	open(t, path)
	db, _ := sql.Open("sqlite", path)
	defer db.Close()
	rows, err := db.Query(`SELECT name FROM pragma_table_info('tasks')`)
	if err != nil {
		t.Fatal(err)
	}
	var cols []string
	for rows.Next() {
		var c string
		rows.Scan(&c)
		cols = append(cols, c)
	}
	rows.Close()
	want := []string{"id", "title", "cwd", "engine", "model", "engine_args", "session_id", "parent_session", "in_place", "prompt", "after", "check_spec", "max_rounds", "round", "merge_into", "phase", "last_check", "note", "created_at", "updated_at", "worker_rules", "outcome", "step", "notified", "notified_at", "notified_phase"}
	if len(cols) != len(want) {
		t.Fatalf("columns %v", cols)
	}
	for i := range want {
		if cols[i] != want[i] {
			t.Errorf("column %d = %s, want %s", i, cols[i], want[i])
		}
	}
	db.Exec(`PRAGMA user_version = 99`)
	if _, err := Open(path); err == nil {
		t.Error("a newer schema must be refused")
	}
}
