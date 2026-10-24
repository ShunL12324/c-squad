package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestLiveFlow runs csquad's flow with the real claude (haiku): a worker
// with --check 'cmd:test -f hello.txt' --merge, and a task queued --after
// it that starts once the merge has landed. Opt-in: CSQUAD_E2E_LIVE=1, with
// CSQUAD_E2E_REPO a disposable repository Claude Code trusts (see TestLive).
// The first task's commit is merged into the repository's current branch.
// The test removes every session, worktree and branch it made, also when it
// fails.
//
// It runs a csquad built from this package (not the test binary): the
// sessions' Stop hook and the detached advance/check processes start it.
func TestLiveFlow(t *testing.T) {
	if os.Getenv("CSQUAD_E2E_LIVE") != "1" {
		t.Skip("set CSQUAD_E2E_LIVE=1 to run against the real claude")
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "csquad-home")
	bin := filepath.Join(root, "bin", "csquad")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") && !strings.HasPrefix(kv, "CSQUAD_") {
			env = append(env, kv)
		}
	}
	env = append(env, "CSQUAD_HOME="+home, "CSQUAD_WAIT_INTERVAL_MS=3000", "CSQUAD_WAIT_GRACE_MS=1000")
	git := func(dir string, args ...string) string {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(env, "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@example.com", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@example.com")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("git %v: %v\n%s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	repo := os.Getenv("CSQUAD_E2E_REPO")
	if repo == "" {
		repo = filepath.Join(root, "repo")
		os.MkdirAll(repo, 0o755)
		git(repo, "init", "-q", "-b", "main")
	}
	if git(repo, "rev-parse", "--verify", "-q", "HEAD") == "" {
		os.WriteFile(filepath.Join(repo, "README"), []byte("csquad live test\n"), 0o644)
		git(repo, "add", "README")
		git(repo, "commit", "-qm", "init")
	}
	target := git(repo, "branch", "--show-current")
	run := func(stdin string, args ...string) result {
		cmd := exec.Command(bin, args...)
		cmd.Env, cmd.Dir, cmd.Stdin = env, repo, strings.NewReader(stdin)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		err := cmd.Run()
		code := 0
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		t.Logf("csquad %v → %d\n%s%s", args, code, out.String(), errb.String())
		return result{code, out.String(), errb.String()}
	}
	type entry struct {
		ID, Phase, State, Session, Note string
	}
	status := func() map[string]entry {
		var list []entry
		json.Unmarshal([]byte(run("", "status", "--all", "--json").stdout), &list)
		out := map[string]entry{}
		for _, e := range list {
			out[e.ID] = e
		}
		return out
	}

	r := run("Create a file hello.txt containing the single line hi, commit it, and finish with a one-line result. Do nothing else.",
		"launch", "--title", "csquad live flow", "--cwd", repo, "--check", "cmd:test -f hello.txt", "--merge", "--", "--model", "haiku")
	m := launchedRe.FindStringSubmatch(r.stdout)
	if r.code != 0 || m == nil {
		t.Fatalf("launch failed (an untrusted repository? see CSQUAD_E2E_REPO)")
	}
	first, session := m[1], m[3]
	r = run("Create a file bye.txt containing the single line bye, commit it, and finish with a one-line result. Do nothing else.",
		"launch", "--title", "csquad live dependent", "--cwd", repo, "--after", "T"+first, "--", "--model", "haiku")
	q := regexp.MustCompile(`^queued T(\d+) `).FindStringSubmatch(r.stdout)
	if r.code != 0 || q == nil {
		t.Fatalf("queueing the dependent failed")
	}
	second := q[1]
	sessions := []string{session}
	ours := func(name string) bool {
		for _, n := range []string{first, second} {
			if strings.HasPrefix(name, "worktree-t"+n+"-") || strings.HasPrefix(name, "task/t"+n+"-") || strings.HasPrefix(filepath.Base(name), "t"+n+"-") {
				return true
			}
		}
		return false
	}
	t.Cleanup(func() {
		out, _ := exec.Command("claude", "agents", "--json", "--all").Output()
		for _, s := range sessions {
			if s != "" && strings.Contains(string(out), s) {
				exec.Command("claude", "stop", s).Run()
				exec.Command("claude", "rm", s).Run()
			}
		}
		for _, line := range strings.Split(git(repo, "worktree", "list", "--porcelain"), "\n") {
			if p, ok := strings.CutPrefix(line, "worktree "); ok && ours(p) {
				git(repo, "worktree", "unlock", p)
				git(repo, "worktree", "remove", "--force", p)
			}
		}
		for _, b := range strings.Fields(git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")) {
			if ours(b) {
				git(repo, "branch", "-D", b)
			}
		}
	})

	// The flow drives it: the Stop hook, and wait's own advancing.
	r = run("", "wait", "T"+first, "--timeout", "15")
	if !strings.HasSuffix(r.stdout, "· finished\n") {
		run("", "status", "--all")
		t.Fatalf("wait: %q", r.stdout)
	}
	if b, err := exec.Command("git", "-C", repo, "show", target+":hello.txt").Output(); err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Errorf("%s:hello.txt: %q %v", target, b, err)
	}
	for _, b := range strings.Fields(git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")) {
		if strings.HasPrefix(b, "worktree-t"+first+"-") || strings.HasPrefix(b, "task/t"+first+"-") {
			t.Errorf("branch %s left", b)
		}
	}
	ev, _ := os.ReadFile(filepath.Join(home, "logs", "events.jsonl"))
	t.Logf("events:\n%s", ev)
	for _, want := range []string{`"from":"running","reason":"the worker stopped; check 1 of 3"`, `"to":"merging"`, `"from":"merging","reason":"merged `} {
		if !strings.Contains(string(ev), want) {
			t.Errorf("events lack %s", want)
		}
	}
	adv, _ := os.ReadFile(filepath.Join(home, "logs", "advance.log"))
	if !strings.Contains(string(adv), "csquad advance --stopped T"+first) {
		t.Logf("the Stop hook did not spawn an advance (wait advanced the flow itself):\n%s", adv)
	} else {
		t.Logf("the Stop hook spawned: advance --stopped T%s", first)
	}

	// The dependent started with what the first task left.
	var dep entry
	deadline := time.Now().Add(3 * time.Minute)
	for {
		dep = status()["T"+second]
		if dep.Session != "" || time.Now().After(deadline) {
			break
		}
		time.Sleep(3 * time.Second)
	}
	if dep.Session == "" {
		t.Fatalf("T%s did not start: %+v", second, dep)
	}
	sessions = append(sessions, dep.Session)
	r = run("", "wait", "T"+second, "--timeout", "10")
	if !regexp.MustCompile(`· (done|idle)\n$`).MatchString(r.stdout) {
		t.Errorf("wait for the dependent: %q", r.stdout)
	}
	if p := run("", "peek", "T"+second, "-n", "40").stdout; !strings.Contains(p, "This task was queued to start after T"+first) || !strings.Contains(p, "into "+target) {
		t.Errorf("the dependent's prompt lacks the first task's outcome:\n%s", p)
	}
	if r := run("", "cancel", "T"+second, "--discard"); r.code != 0 {
		t.Errorf("cancel: %+v", r)
	}
	out, _ := exec.Command("claude", "agents", "--json", "--all").Output()
	for _, s := range sessions {
		if strings.Contains(string(out), s) {
			t.Errorf("session %s still listed", s)
		}
	}
	fmt.Fprintln(os.Stderr, "live flow: T"+first+" merged into "+target+", T"+second+" started after it")
}
