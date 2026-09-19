package squad

import (
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/process"
)

func must(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func TestHelpPreservesReviewAndIndependentBlockers(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhaseInReview, Owner: "a", Participants: []string{"a", "b"}, Milestones: []Milestone{{"decision", true, MilestoneStateAwaitingApproval}}}
		return nil
	}))
	for _, id := range []string{"a", "b"} {
		must(t, helpCommand(st, id, []string{"request"}, options{"task": "T1", "text": "need decision"}))
	}
	s, _ := st.read()
	if len(s.Tasks["T1"].Blockers) != 3 {
		t.Fatal("missing blockers")
	}
	if e := taskCommand(st, "master", []string{"approve", "T1"}, options{}); e == nil {
		t.Fatal("approved blocked task")
	}
	var ids []string
	for id := range s.Questions {
		ids = append(ids, id)
	}
	must(t, helpCommand(st, "master", []string{"answer", ids[0]}, options{"text": "yes"}))
	s, _ = st.read()
	if s.Tasks["T1"].State != TaskPhaseInReview || len(s.Tasks["T1"].Blockers) != 2 {
		t.Fatal("answer changed phase or cleared unrelated blockers")
	}
	must(t, taskCommand(st, "master", []string{"gate", "T1"}, options{"name": "decision"}))
	s, _ = st.read()
	if len(s.Tasks["T1"].Blockers) != 1 {
		t.Fatal("gate cleared question")
	}
	must(t, helpCommand(st, "master", []string{"answer", ids[1]}, options{"text": "yes"}))
	s, _ = st.read()
	if s.Tasks["T1"].State != TaskPhaseInReview || len(s.Tasks["T1"].Blockers) != 0 {
		t.Fatal("review phase lost")
	}
	must(t, taskCommand(st, "master", []string{"approve", "T1"}, options{}))
	if e := helpCommand(st, "a", []string{"request"}, options{"task": "T1", "text": "late"}); e == nil {
		t.Fatal("reopened done task")
	}
}
func TestDispatchExplicitOwnershipAndIdempotentCreation(t *testing.T) {
	st := testStore(t)
	for i := 0; i < 2; i++ {
		must(t, taskCommand(st, "master", []string{"create", "one"}, options{"acceptance": "done", "request-id": "one"}))
	}
	s, _ := st.read()
	if len(s.Tasks) != 1 || len(s.Messages) != 0 {
		t.Fatal("duplicate task or broadcast on assigned creation")
	}
	var id string
	for k := range s.Tasks {
		id = k
	}
	if e := taskCommand(st, "a", []string{"claim", id}, options{}); e == nil {
		t.Fatal("claimed reserved task")
	}
	if e := taskCommand(st, "master", []string{"assign", id}, options{"to": "b,a"}); e == nil {
		t.Fatal("inferred owner from order")
	}
	must(t, taskCommand(st, "master", []string{"assign", id}, options{"to": "b,a", "owner": "a"}))
	s, _ = st.read()
	if s.Tasks[id].Owner != "a" {
		t.Fatal("wrong owner")
	}
	must(t, taskCommand(st, "master", []string{"create", "two"}, options{"acceptance": "done", "dispatch": "open"}))
	s, _ = st.read()
	for k := range s.Tasks {
		if k != id {
			if e := taskCommand(st, "a", []string{"claim", k}, options{}); e == nil {
				t.Fatal("owned two tasks")
			}
		}
	}
}
func TestReplyAndSendIdempotency(t *testing.T) {
	st := testStore(t)
	for i := 0; i < 2; i++ {
		must(t, messageCommand(st, "master", []string{"message", "send", "a"}, options{"text": "hello", "request-id": "hello"}))
	}
	s, _ := st.read()
	if len(s.Messages) != 1 {
		t.Fatal("duplicate send")
	}
	id := s.Messages[0].ID
	for i := 0; i < 2; i++ {
		must(t, messageCommand(st, "a", []string{"reply", id}, options{"text": "ok"}))
	}
	s, _ = st.read()
	if len(s.Messages) != 2 {
		t.Fatal("duplicate reply")
	}
}
func TestDeliveryRetriesFailureAndStopsAfterAck(t *testing.T) {
	st := testStore(t)
	sock := filepath.Join(t.TempDir(), "inbox.sock")
	var id string
	must(t, st.update(func(s *State) error {
		m := s.Members["a"]
		m.EngineID = "test"
		m.Peer = sock
		id = s.message("master", "a", "", "hello", "").ID
		return nil
	}))
	if e := st.deliver(id); e == nil {
		t.Fatal("expected absent socket failure")
	}
	l, e := net.Listen("unix", sock)
	must(t, e)
	defer l.Close()
	received := make(chan string, 2)
	go func() {
		c, e := l.Accept()
		if e != nil {
			return
		}
		defer c.Close()
		var v map[string]any
		json.NewDecoder(c).Decode(&v)
		b, _ := json.Marshal(v)
		received <- string(b)
	}()
	must(t, st.update(func(s *State) error {
		s.Messages[0].Attempt = time.Now().Add(-time.Hour).Format(time.RFC3339Nano)
		return nil
	}))
	must(t, st.syncMessages())
	select {
	case v := <-received:
		if !strings.Contains(v, id) {
			t.Fatal("message identity missing")
		}
	case <-time.After(time.Second):
		t.Fatal("no delivery")
	}
	must(t, messageCommand(st, "a", []string{"message", "ack", id}, options{}))
	must(t, st.syncMessages())
	s, _ := st.read()
	if s.Messages[0].State != DeliveryStateAcknowledged || s.Messages[0].Attempts != 2 {
		t.Fatal("ack retried")
	}
}
func TestStaleGenerationFencedInTransaction(t *testing.T) {
	st := testStore(t)
	st.Actor = "a"
	st.Generation = 1
	must(t, st.update(func(s *State) error { s.Members["a"].Generation = 2; return nil }))
	if e := st.update(func(s *State) error { s.ID = "bad"; return nil }); e == nil {
		t.Fatal("old worker wrote state")
	}
}
func TestQuestionHookRoutesToMaster(t *testing.T) {
	st := testStore(t)
	for _, tool := range []string{"AskUserQuestion", "functions.request_user_input_async", "request_user_input"} {
		b, _ := json.Marshal(map[string]any{"hook_event_name": "PreToolUse", "tool_name": tool, "session_id": "test", "tool_input": map[string]string{"question": "probe"}})
		must(t, hookInput(st, "a", 1, strings.NewReader(string(b))))
	}
	s, _ := st.read()
	if len(s.Questions) != 3 || len(s.Messages) != 3 {
		t.Fatal("question not routed")
	}
}
func TestStopTreeTerminatesDescendant(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child")
	c := exec.Command("sh", "-c", `sleep 60 & echo $! > "$1"; wait`, "sh", marker)
	must(t, c.Start())
	defer c.Process.Kill()
	defer c.Wait()
	deadline := time.Now().Add(time.Second)
	for {
		if _, e := os.Stat(marker); e == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("child not started")
		}
		time.Sleep(10 * time.Millisecond)
	}
	all, e := process.Snapshot()
	must(t, e)
	tree := process.Descendants(all, c.Process.Pid)
	if len(tree) < 2 {
		t.Fatal("no child to exercise")
	}
	must(t, process.StopTree(c.Process.Pid, all[c.Process.Pid].Start))
	all, e = process.Snapshot()
	must(t, e)
	for _, p := range tree {
		if process.Alive(p, all) {
			t.Fatalf("survivor %d", p.PID)
		}
	}
}
func TestReconcileGitAdvancedBeforeLedgerCommit(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	root := s.Root
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		_, e := git(root, a...)
		must(t, e)
	}
	must(t, taskCommand(st, "master", []string{"create", "code"}, options{"code": "true", "acceptance": "pass"}))
	s, _ = st.read()
	var task *Task
	for _, v := range s.Tasks {
		task = v
	}
	must(t, taskCommand(st, "master", []string{"assign", task.ID}, options{"owner": "a", "to": "a,b"}))
	_, e := git(task.Workspace, "commit", "--allow-empty", "-m", "candidate")
	must(t, e)
	must(t, taskCommand(st, "a", []string{"submit", task.ID}, options{"summary": "done"}))
	sha, e := git(task.Workspace, "rev-parse", "HEAD")
	must(t, e)
	for _, kind := range []string{"review", "test"} {
		must(t, taskCommand(st, "b", []string{"evidence", task.ID}, options{"kind": kind, "sha": sha, "passed": "true", "summary": "pass"}))
	}
	must(t, taskCommand(st, "master", []string{"approve", task.ID}, options{}))
	must(t, st.update(func(s *State) error {
		v := s.Tasks[task.ID]
		a := *v.Approval
		v.MergeIntent = &a
		v.State = TaskPhaseMerging
		return nil
	}))
	// Exactly the durable state left if the process dies after Git succeeds.
	_, e = git(root, "merge", "--ff-only", sha)
	must(t, e)
	must(t, st.reconcile())
	must(t, st.reconcile())
	s, _ = st.read()
	v := s.Tasks[task.ID]
	if v.State != TaskPhaseDone || v.MergeCommit != sha || v.MergeIntent != nil {
		t.Fatal("did not reconcile exactly once")
	}
	must(t, mergeCommand(st, "master", task.ID))
}

func TestWorkspaceIntentRecovery(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		_, e := git(s.Root, a...)
		must(t, e)
	}
	base, e := git(s.Root, "rev-parse", "HEAD")
	must(t, e)
	path := filepath.Join(st.Dir, "worktrees", "T1")
	must(t, st.update(func(s *State) error {
		s.Tasks["T1"] = &Task{ID: "T1", State: TaskPhasePreparing, Branch: "csquad/test/T1", Base: base, Workspace: path}
		return nil
	}))
	// Simulate a crash after branch creation, before git worktree add completed.
	_, e = git(s.Root, "branch", "csquad/test/T1", base)
	must(t, e)
	must(t, st.prepareWorkspace("T1"))
	must(t, st.prepareWorkspace("T1"))
	s, _ = st.read()
	if s.Tasks["T1"].State != TaskPhaseReady {
		t.Fatal("workspace did not recover")
	}
	head, e := git(path, "rev-parse", "HEAD")
	must(t, e)
	if head != base {
		t.Fatal("wrong worktree base")
	}
}
func TestNonGitResearchAndCodeRejection(t *testing.T) {
	st := testStore(t)
	must(t, taskCommand(st, "master", []string{"create", "research"}, options{"acceptance": "report"}))
	if e := taskCommand(st, "master", []string{"create", "code"}, options{"acceptance": "report", "code": "true"}); e == nil {
		t.Fatal("code without Git accepted")
	}
	s, _ := st.read()
	if len(s.Tasks) != 1 {
		t.Fatal("failed code task leaked state")
	}
}

// Subprocess entry point for killing an actual CLI between Git and SQLite.
func TestCrashCLIHelper(t *testing.T) {
	if os.Getenv("CSQUAD_CRASH_HELPER") != "1" {
		return
	}
	at := 0
	for i, a := range os.Args {
		if a == "--" {
			at = i + 1
			break
		}
	}
	if e := Run(os.Args[at:]); e != nil {
		os.Exit(2)
	}
	os.Exit(0)
}
func TestActualCLICrashAfterGitMerge(t *testing.T) {
	st := testStore(t)
	s, _ := st.read()
	for _, a := range [][]string{{"init", "-b", "main"}, {"config", "user.name", "Test"}, {"config", "user.email", "test@example.invalid"}, {"commit", "--allow-empty", "-m", "base"}} {
		_, e := git(s.Root, a...)
		must(t, e)
	}
	must(t, taskCommand(st, "master", []string{"create", "code"}, options{"code": "true", "acceptance": "pass"}))
	s, _ = st.read()
	var task *Task
	for _, v := range s.Tasks {
		task = v
	}
	must(t, taskCommand(st, "master", []string{"assign", task.ID}, options{"owner": "a", "to": "a,b"}))
	_, e := git(task.Workspace, "commit", "--allow-empty", "-m", "candidate")
	must(t, e)
	must(t, taskCommand(st, "a", []string{"submit", task.ID}, options{"summary": "done"}))
	sha, e := git(task.Workspace, "rev-parse", "HEAD")
	must(t, e)
	for _, kind := range []string{"review", "test"} {
		must(t, taskCommand(st, "b", []string{"evidence", task.ID}, options{"kind": kind, "sha": sha, "passed": "true", "summary": "pass"}))
	}
	must(t, taskCommand(st, "master", []string{"approve", task.ID}, options{}))
	realGit, e := exec.LookPath("git")
	must(t, e)
	dir := t.TempDir()
	script := "#!/bin/sh\n" + shellQuote(realGit) + " \"$@\"\nresult=$?\nif [ \"$1\" = merge ] && [ \"$result\" = 0 ]; then kill -KILL \"$PPID\"; fi\nexit \"$result\"\n"
	must(t, os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0700))
	c := exec.Command(os.Args[0], "-test.run=^TestCrashCLIHelper$", "--", "--team", st.Dir, "task", "merge", task.ID)
	c.Env = append(os.Environ(), "CSQUAD_CRASH_HELPER=1", "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if e = c.Run(); e == nil {
		t.Fatal("expected killed CLI")
	}
	s, _ = st.read()
	if s.Tasks[task.ID].State != TaskPhaseMerging || s.Tasks[task.ID].MergeIntent == nil {
		t.Fatal("durable intent missing after crash")
	}
	head, e := git(s.Root, "rev-parse", "HEAD")
	must(t, e)
	if head != sha {
		t.Fatal("Git did not advance before crash")
	}
	must(t, st.reconcile())
	s, _ = st.read()
	if s.Tasks[task.ID].State != TaskPhaseDone {
		t.Fatal("actual crash not recovered")
	}
}

func TestMissingTmuxSessionDoesNotMatchPrefix(t *testing.T) {
	if _, e := exec.LookPath("tmux"); e != nil {
		t.Skip("tmux unavailable")
	}
	st := testStore(t)
	// Keep socket paths short enough for Unix sockets even under long test names.
	dir, e := os.MkdirTemp("", "csq-test-")
	must(t, e)
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "s")
	_, e = process.Run("", "tmux", "-f", "/dev/null", "-S", socket, "new-session", "-d", "-s", "csq-test-a-extra", "sleep", "60")
	must(t, e)
	defer process.Run("", "tmux", "-S", socket, "kill-server")
	must(t, st.update(func(s *State) error { s.Socket = socket; s.Members["a"].Session = "csq-test-a"; return nil }))
	must(t, killMember(st, "a"))
	s, _ := st.read()
	_, e = tm(s, "has-session", "-t", "=csq-test-a-extra")
	must(t, e)
	_, e = tm(s, "set-window-option", "-t", "=csq-test-a-extra:", "remain-on-exit", "on")
	must(t, e)
	_, e = tm(s, "set-option", "-t", "=csq-test-a-extra", "@csquad_team", st.Dir)
	must(t, e)
	alive, e := tm(s, "display-message", "-p", "-t", "=csq-test-a-extra:", "#{pane_dead}")
	must(t, e)
	if alive != "0" {
		t.Fatal("wrong exact pane target")
	}
	_, e = tm(s, "capture-pane", "-p", "-t", "=csq-test-a-extra:")
	must(t, e)
}
func TestNativeConfigMetadataAndFailureExit(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
while IFS= read -r line; do
case "$line" in
*'"initialize"'*) printf '%s\n' '{"id":1,"result":{}}' ;;
*'"config/read"'*)
printf '%s\n' '{"id":2,"result":{"config":{"developer_instructions":"ORIGINAL","hooks":{"SessionStart":[{"hooks":[{"type":"command","command":"echo keep","timeout":3}]}],"state":{"native-id":{"trusted_hash":"opaque"}}}},"layers":[]}}'
;; esac
done
`
	must(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0700))
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, e := codexConfig(dir, false)
	must(t, e)
	if cfg.Instructions != "ORIGINAL" {
		t.Fatal("native instructions dropped")
	}
	var groups []any
	must(t, json.Unmarshal(cfg.Hooks["SessionStart"], &groups))
	v, e := tomlValue(groups)
	must(t, e)
	if !strings.Contains(v, "echo keep") {
		t.Fatal("native hook dropped")
	}
	bad := strings.Replace(script, `"SessionStart":[{"hooks":[{"type":"command","command":"echo keep","timeout":3}]}],"state":{"native-id":{"trusted_hash":"opaque"}}`, `"state":{"native-id":{"enabled":false}}`, 1)
	must(t, os.WriteFile(filepath.Join(dir, "codex"), []byte(bad), 0700))
	before := time.Now()
	if _, e = codexConfig(dir, false); e == nil {
		t.Fatal("disabled native hook was silently enabled")
	}
	if time.Since(before) > 3*time.Second {
		t.Fatal("invalid config hung resolver")
	}
}
