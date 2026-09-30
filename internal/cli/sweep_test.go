package cli

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/claude"
	"github.com/ShunL12324/c-squad/internal/queue"
)

func TestSweep(t *testing.T) {
	store, err := queue.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	add := func(session string) int64 {
		task, _ := store.Add(queue.Task{Prompt: "x", Cwd: "/"})
		if ok, _ := store.Claim(task.ID); !ok {
			t.Fatal("claim")
		}
		if session != "" {
			_ = store.SetSession(task.ID, session)
		}
		return task.ID
	}
	crashed, removed, live := add(""), add("gone1"), add("live1")
	sessions := map[string]claude.Session{"live1": {State: claude.StateDone}}
	// Within the launch grace nothing changes.
	if err := sweep(store, sessions, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(crashed); got.State != queue.Launched {
		t.Fatalf("swept too early: %s", got.State)
	}
	if err := sweep(store, sessions, time.Now().Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[int64]queue.State{crashed: queue.Failed, removed: queue.Finished, live: queue.Launched} {
		if got, _ := store.Get(id); got.State != want {
			t.Fatalf("T%d = %s, want %s", id, got.State, want)
		}
	}
	// Pruning keeps recently ended tasks.
	if tasks, _ := store.List(""); len(tasks) != 3 {
		t.Fatalf("pruned recent tasks: %d left", len(tasks))
	}
	if err := sweep(store, sessions, time.Now().Add(8*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if tasks, _ := store.List(""); len(tasks) != 1 || tasks[0].ID != live {
		t.Fatalf("after a week: %+v", tasks)
	}
}
