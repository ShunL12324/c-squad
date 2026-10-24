package main

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/check"
	"github.com/ShunL12324/c-squad/internal/store"
)

// --check codex: the detached check starts a read-only reviewer thread
// "T1 · check 1" on the (fake) Codex daemon with the verdict schema, reads
// its answer, archives it, and the flow moves on.
func TestCodexReviewerThread(t *testing.T) {
	h, srv, repo := codexHome(t)
	n := launchIn(t, h, repo, "fix", "--check", "codex:gpt-r")
	work(t, h, repo, n, map[string]string{"b.txt": "b\n"})
	srv.Reset()
	advance(t, h, "CSQUAD_CHECK_POLL_MS=50")
	var reviewer string
	waitUntil(t, "the reviewer thread", 20*time.Second, func() bool {
		for _, c := range srv.Calls() {
			if c.Method == "thread/name/set" && c.Params["name"] == "T1 · check 1" {
				reviewer, _ = c.Params["threadId"].(string)
			}
		}
		return reviewer != ""
	})
	start, turn := srv.Last("thread/start"), srv.Last("turn/start")
	if start["sandbox"] != "read-only" || start["approvalPolicy"] != "never" || start["model"] != "gpt-r" || start["developerInstructions"] != nil || start["cwd"] != filepath.Join(repo, ".claude", "worktrees", "t1-fix") {
		t.Errorf("thread/start %v", start)
	}
	var schema any
	json.Unmarshal(check.Schema, &schema)
	if !reflect.DeepEqual(turn["outputSchema"], schema) {
		t.Errorf("outputSchema %v", turn["outputSchema"])
	}
	srv.Complete(reviewer, `{"verdict":"pass","summary":"looks right","findings":[{"severity":"minor","file":"b.txt","summary":"no newline check"}]}`)
	task := waitPhase(t, h, n, store.Ready)
	if !strings.Contains(task.LastCheck, `"summary":"looks right"`) {
		t.Errorf("%s", brief(task))
	}
	waitUntil(t, "the reviewer archived", 10*time.Second, func() bool {
		for _, c := range srv.Calls() {
			if c.Method == "thread/archive" && c.Params["threadId"] == reviewer {
				return true
			}
		}
		return false
	})
	// The reviewer's Stop event is no task's: the hook does nothing.
	r := ok(t, csq(t, h, opts{stdin: `{"session_id":"` + reviewer + `"}`}, "hook", "codex-stop"))
	if r.stdout != "" {
		t.Errorf("%+v", r)
	}
}
