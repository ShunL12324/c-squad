package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
)

// TestLiveThread runs one trivial thread against the real Codex daemon
// (CSQUAD_CODEX_LIVE=1): start, state, peek, send, usage, fork, archive. It
// uses the user's Codex login and configuration, starts the daemon when it is
// not running, and deletes its threads at the end.
func TestLiveThread(t *testing.T) {
	if os.Getenv("CSQUAD_CODEX_LIVE") != "1" {
		t.Skip("set CSQUAD_CODEX_LIVE=1 to run against the real codex")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	e := New(engine.Config{Env: os.Environ()})
	dir := t.TempDir()
	args, err := e.CheckArgs([]string{"--effort", "low", "--sandbox", "read-only"}, dir)
	if err != nil {
		t.Fatal(err)
	}
	res, err := e.Start(ctx, engine.StartSpec{ID: 999, Title: "csquad live test", Cwd: dir, Args: args,
		Prompt: "Reply with exactly the word pong and nothing else. Do not run any command."})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	t.Logf("thread %s", id)
	threads := []string{id}
	defer func() {
		c, err := e.dial(context.Background())
		if err != nil {
			t.Errorf("cleanup: %v", err)
			return
		}
		defer c.close()
		// Forks first: a thread that a fork's history references cannot go.
		for i := len(threads) - 1; i >= 0; i-- {
			id := threads[i]
			_ = c.call(context.Background(), "thread/archive", map[string]any{"threadId": id}, nil)
			if err := c.call(context.Background(), "thread/delete", map[string]any{"threadId": id}, nil); err != nil {
				t.Errorf("cleanup: delete %s: %v", id, err)
			}
		}
	}()
	waitOn := func(id, want string) {
		t.Helper()
		var seen []string
		for i := 0; i < 120; i++ {
			got, err := e.States(ctx, []string{id})
			if err != nil {
				t.Fatal(err)
			}
			s := got[id].State
			if len(seen) == 0 || seen[len(seen)-1] != s {
				seen = append(seen, s)
			}
			if s == want && i > 0 {
				t.Logf("states %v", seen)
				return
			}
			time.Sleep(2 * time.Second)
		}
		t.Fatalf("never %s: %v", want, seen)
	}
	waitFor := func(want string) { t.Helper(); waitOn(id, want) }
	waitFor(engine.Idle)
	msgs, err := e.Last(ctx, id, 4)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(engine.LastAssistant(msgs)), "pong") || msgs[0].Role != "user" {
		t.Fatalf("peek %+v", msgs)
	}
	// A follow-up while a turn runs steers it.
	if _, err := e.Send(ctx, id, "Run the shell command `sleep 10`, then reply with exactly the word done."); err != nil {
		t.Fatal(err)
	}
	waitFor(engine.Working)
	for i := 0; i < 30; i++ {
		got, _ := e.States(ctx, []string{id})
		c, err := e.dial(ctx)
		if err != nil {
			t.Fatal(err)
		}
		turn, _ := e.newest(ctx, c, id)
		c.close()
		if got[id].State == engine.Working && turn != nil && turn.Status == "inProgress" && i > 0 {
			break
		}
		time.Sleep(time.Second)
	}
	how, err := e.Send(ctx, id, "After that, also append the word steered on a second line.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("send while working: %s", how)
	if !strings.Contains(how, "running turn") {
		t.Errorf("not steered: %s", how)
	}
	waitFor(engine.Idle)
	msgs, err = e.Last(ctx, id, 0)
	if err != nil || !strings.Contains(strings.ToLower(engine.LastAssistant(msgs)), "steered") {
		t.Errorf("peek after steer %+v %v", msgs, err)
	}
	how, err = e.Send(ctx, id, "Now reply with exactly the word ping and nothing else.")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("send: %s", how)
	waitFor(engine.Idle)
	msgs, err = e.Last(ctx, id, 0)
	if err != nil || !strings.Contains(strings.ToLower(engine.LastAssistant(msgs)), "ping") {
		t.Fatalf("peek after send %+v %v", msgs, err)
	}
	// Usage, from the rollout's newest token_count event.
	u, err := e.Usage(ctx, id, filepath.Join(t.TempDir(), "T999.read.json"))
	t.Logf("usage %+v %v", u, err)
	if err != nil || !u.Known || u.Fresh() <= 0 || u.ContextMax <= 0 || u.Activity.IsZero() {
		t.Errorf("usage %+v %v", u, err)
	}
	// A fork inherits the conversation; its rules come in the first turn.
	fork, err := e.Start(ctx, engine.StartSpec{ID: 998, Title: "csquad live fork", Cwd: dir, Args: args, Fork: id,
		Rules:  "You are a forked test task. Answer tersely.",
		Prompt: "Which single word did you reply with in your last answer? Reply with exactly that word and nothing else."})
	if err != nil {
		t.Fatal(err)
	}
	threads = append(threads, fork.SessionID)
	t.Logf("fork %s", fork.SessionID)
	waitOn(fork.SessionID, engine.Idle)
	msgs, err = e.Last(ctx, fork.SessionID, 0)
	if err != nil || !strings.Contains(strings.ToLower(engine.LastAssistant(msgs)), "ping") {
		t.Errorf("the fork does not know the conversation: %+v %v", msgs, err)
	}
	fu, err := e.Usage(ctx, fork.SessionID, filepath.Join(t.TempDir(), "T998.read.json"))
	t.Logf("fork usage %+v %v", fu, err)
	how, err = e.Remove(ctx, id, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("remove: %s", how)
	if how, err := e.Remove(ctx, id, nil); err != nil || !strings.Contains(how, "already removed") {
		t.Errorf("remove twice: %q %v", how, err)
	}
	got, err := e.States(ctx, []string{id, "019a0000-0000-7000-8000-000000000000"})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after archive: %+v", got)
	if got["019a0000-0000-7000-8000-000000000000"].State != engine.Gone {
		t.Errorf("unknown thread: %+v", got)
	}
}
