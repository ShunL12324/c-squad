package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync"
	"testing"
)

func TestClaimNoticeSendsEachChangeOnce(t *testing.T) {
	ctx := context.Background()
	s := open(t, filepath.Join(t.TempDir(), "csquad.db"))
	task, _ := s.Insert(ctx, Task{Title: "a", Cwd: "/x", Engine: "claude", Prompt: "p", Phase: Running})
	claim := func(tk Task, class string, at int64, phase bool) bool {
		t.Helper()
		ok, err := s.ClaimNotice(ctx, tk, class, at, phase)
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if claim(task, "", 10, false) {
		t.Error("a quiet state sends nothing")
	}
	if !claim(task, "needs_input", 20, false) {
		t.Error("needs input: the first observer sends")
	}
	if claim(task, "needs_input", 21, false) {
		t.Error("the same change seen again sends nothing")
	}
	if claim(task, "", 15, false) {
		t.Error("an observation older than the recorded one counts for nothing")
	}
	if got, _ := s.Get(ctx, task.ID); got.Notified != "needs_input" {
		t.Errorf("an older observation overwrote a newer one: %q", got.Notified)
	}
	claim(task, "", 30, false)
	if !claim(task, "needs_input", 40, false) {
		t.Error("needs input again, after work, sends again")
	}

	// A phase event is sent once per version of the row.
	ready := task
	ready.Phase = Ready
	ready, err := s.Transition(ctx, task, ready)
	if err != nil {
		t.Fatal(err)
	}
	if claim(task, "", 50, false) {
		t.Error("an observation of the old row sends nothing")
	}
	if got, _ := s.Get(ctx, task.ID); got.Notified != "needs_input" {
		t.Errorf("an observation of the old row was recorded: %q", got.Notified)
	}
	if !claim(ready, "ready", 60, true) {
		t.Error("ready sends")
	}
	if !claim(ready, "needs_input", 70, false) {
		t.Error("a held message after ready sends needs input")
	}
	if claim(ready, "ready", 80, true) {
		t.Error("ready again for the same version sends nothing")
	}
	needs := ready
	needs.Phase = NeedsYou
	needs, _ = s.Transition(ctx, ready, needs)
	if !claim(needs, "needs_you", 90, true) {
		t.Error("needs you sends")
	}
	fixing := needs
	fixing.Phase = Fixing
	fixing, _ = s.Transition(ctx, needs, fixing)
	again := fixing
	again.Phase = NeedsYou
	again, _ = s.Transition(ctx, fixing, again)
	if !claim(again, "needs_you", 100, true) {
		t.Error("needs you of a new version sends, even when the quiet phase between was never observed")
	}
}

func TestClaimNoticeConcurrentObserversSendOnce(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "csquad.db")
	first := open(t, path)
	task, _ := first.Insert(ctx, Task{Title: "a", Cwd: "/x", Engine: "claude", Prompt: "p", Phase: Running})
	// Each observer has its own connection, as each csquad process has.
	stores := []*Store{first}
	for range 7 {
		stores = append(stores, open(t, path))
	}
	for round, class := range []string{"needs_input", "", "done", "", "needs_input"} {
		var wg sync.WaitGroup
		var mu sync.Mutex
		sent := 0
		for i, s := range stores {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, err := s.ClaimNotice(ctx, task, class, int64(100*round+i+1), false)
				if err != nil {
					t.Error(err)
				}
				if ok {
					mu.Lock()
					sent++
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
		want := 1
		if class == "" {
			want = 0
		}
		if sent != want {
			t.Errorf("%q: %d observers sent, want %d", class, sent, want)
		}
	}
}

func TestMigrationCountsWhatTasksShowAsAnnounced(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "csquad.db")
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migrations[:2] {
		if _, err := db.Exec(m); err != nil {
			t.Fatal(err)
		}
	}
	db.Exec(`PRAGMA user_version = 2`)
	for i, row := range [][3]string{{"ready", "", ""}, {"needs_you", "codex", ""}, {"running", "", ""}, {"running", "codex", ""}, {"fixing", "codex", "main"}} {
		if _, err := db.Exec(`INSERT INTO tasks (id, title, cwd, engine, prompt, phase, check_spec, merge_into, created_at, updated_at) VALUES (?, 't', '/x', 'claude', 'p', ?, ?, ?, 1, ?)`,
			i+1, row[0], row[1], row[2], 1000+i); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	s := open(t, path)
	want := map[int]string{1: "ready", 2: "needs_you", 3: "done", 4: "", 5: ""}
	for id, class := range want {
		got, err := s.Get(ctx, id)
		if err != nil || got.Notified != class {
			t.Errorf("T%d: notified %q, want %q (%v)", id, got.Notified, class, err)
		}
	}
	ready, _ := s.Get(ctx, 1)
	if ok, _ := s.ClaimNotice(ctx, ready, "ready", 5000, true); ok {
		t.Error("a task ready before the update sends ready again")
	}
	plain, _ := s.Get(ctx, 3)
	if ok, _ := s.ClaimNotice(ctx, plain, "done", 5000, false); ok {
		t.Error("a task done before the update sends done again")
	}
	if ok, _ := s.ClaimNotice(ctx, plain, "needs_input", 6000, false); !ok {
		t.Error("a new change after the update sends")
	}
}
