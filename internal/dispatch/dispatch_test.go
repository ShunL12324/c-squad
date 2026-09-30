package dispatch

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/claude"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/queue"
)

type fake struct {
	sessions map[string]claude.Session
	launched []claude.LaunchOptions
	failFor  string
}

func (f *fake) dispatcher(store *queue.Store, slots int) *Dispatcher {
	return &Dispatcher{
		Store:    store,
		Config:   config.Config{Slots: slots, Model: "sonnet"},
		Sessions: func(context.Context) (map[string]claude.Session, error) { return f.sessions, nil },
		Launch: func(_ context.Context, o claude.LaunchOptions) (string, error) {
			if o.Prompt == f.failFor {
				return "", errors.New("boom")
			}
			f.launched = append(f.launched, o)
			id := fmt.Sprintf("s%d", len(f.launched))
			f.sessions[id] = claude.Session{ID: id, State: claude.StateWorking}
			return id, nil
		},
		Trust: func(string) error { return nil },
	}
}

func TestStepRespectsSlotsAndFreesFinishedSessions(t *testing.T) {
	store, err := queue.Open(filepath.Join(t.TempDir(), "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	for i := range 5 {
		if _, err := store.Add(queue.Task{Prompt: fmt.Sprint("task ", i), Cwd: "/w"}); err != nil {
			t.Fatal(err)
		}
	}
	f := &fake{sessions: map[string]claude.Session{}, failFor: "task 3"}
	d := f.dispatcher(store, 2)
	remaining, err := d.Step(context.Background())
	if err != nil || remaining != 3 || len(f.launched) != 2 {
		t.Fatalf("first step: remaining %d, launched %d, %v", remaining, len(f.launched), err)
	}
	if f.launched[0].Model != "sonnet" || f.launched[0].Name != "T1 · task 0" || f.launched[0].SystemPrompt == "" {
		t.Fatalf("launch options = %+v", f.launched[0])
	}
	if remaining, _ := d.Step(context.Background()); remaining != 3 {
		t.Fatalf("launched past the slot limit; remaining %d", remaining)
	}
	// A blocked session keeps its slot; a done one frees it.
	f.sessions["s1"] = claude.Session{ID: "s1", State: claude.StateBlocked}
	f.sessions["s2"] = claude.Session{ID: "s2", State: claude.StateDone}
	remaining, _ = d.Step(context.Background())
	if remaining != 2 || len(f.launched) != 3 {
		t.Fatalf("after one slot freed: remaining %d, launched %d", remaining, len(f.launched))
	}
	// Session s3 finishes; the next task fails to launch and the one after runs.
	f.sessions["s3"] = claude.Session{ID: "s3", State: claude.StateDone}
	remaining, _ = d.Step(context.Background())
	if remaining != 1 || len(f.launched) != 3 {
		t.Fatalf("failed launch: remaining %d, launched %d", remaining, len(f.launched))
	}
	failed, _ := store.Get(4)
	if failed.State != queue.Failed || failed.Error != "boom" {
		t.Fatalf("failed task = %+v", failed)
	}
	remaining, _ = d.Step(context.Background())
	if remaining != 0 || len(f.launched) != 4 || f.launched[3].Prompt != "task 4" {
		t.Fatalf("task after the failed launch: remaining %d, launched %+v", remaining, f.launched)
	}
}

func TestOccupiedCountsRecentLaunchesNotYetListed(t *testing.T) {
	now := time.Now()
	launched := []queue.Task{
		{Session: "", LaunchedAt: now},
		{Session: "old", LaunchedAt: now.Add(-time.Hour)},
		{Session: "live", LaunchedAt: now.Add(-time.Hour)},
	}
	sessions := map[string]claude.Session{"live": {State: claude.StateWorking}}
	if got := Occupied(launched, sessions, now); got != 2 {
		t.Fatalf("Occupied = %d, want 2", got)
	}
}

func TestRunIsSingleAndExitsWhenQueueEmpty(t *testing.T) {
	dir := t.TempDir()
	store, err := queue.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	f := &fake{sessions: map[string]claude.Session{}}
	if err := f.dispatcher(store, 1).Run(context.Background(), dir, time.Millisecond); err != nil {
		t.Fatalf("empty queue: %v", err)
	}
	if Running(dir) {
		t.Fatal("lock still held after exit")
	}
}

// A task queued while the dispatcher is finishing its last step must still be
// launched, since that add saw a running dispatcher and started none.
func TestRunLaunchesTaskQueuedDuringLastStep(t *testing.T) {
	dir := t.TempDir()
	store, err := queue.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.Add(queue.Task{Prompt: "first", Cwd: "/w"}); err != nil {
		t.Fatal(err)
	}
	f := &fake{sessions: map[string]claude.Session{}}
	d := f.dispatcher(store, 5)
	launch := d.Launch
	d.Launch = func(ctx context.Context, o claude.LaunchOptions) (string, error) {
		if o.Prompt == "first" {
			if _, err := store.Add(queue.Task{Prompt: "late", Cwd: "/w"}); err != nil {
				t.Fatal(err)
			}
		}
		return launch(ctx, o)
	}
	if err := d.Run(context.Background(), dir, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if len(f.launched) != 2 || f.launched[1].Prompt != "late" {
		t.Fatalf("launched %+v", f.launched)
	}
}

func TestRunGivesUpOnPersistentErrors(t *testing.T) {
	dir := t.TempDir()
	store, err := queue.Open(filepath.Join(dir, "q.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.Add(queue.Task{Prompt: "x", Cwd: "/w"}); err != nil {
		t.Fatal(err)
	}
	clock := time.Now()
	calls := 0
	d := &Dispatcher{
		Store:  store,
		Config: config.Config{Slots: 1},
		Sessions: func(context.Context) (map[string]claude.Session, error) {
			calls++
			return nil, errors.New("claude missing")
		},
		now: func() time.Time { clock = clock.Add(30 * time.Second); return clock },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Run(ctx, dir, time.Millisecond); err == nil || errors.Is(err, context.DeadlineExceeded) || calls < 3 {
		t.Fatalf("Run = %v after %d calls", err, calls)
	}
	if queued, _ := store.List(queue.Queued); len(queued) != 1 {
		t.Fatal("gave up by dropping the queued task")
	}
	if Running(dir) {
		t.Fatal("lock held after giving up")
	}
}
