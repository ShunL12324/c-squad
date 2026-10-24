package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestLive runs one trivial task with the real claude (haiku): launch, wait,
// status, peek, merge, finish, with a temporary CSQUAD_HOME. Opt-in:
// CSQUAD_E2E_LIVE=1.
//
// Claude Code starts background sessions only in a repository whose trust
// prompt was accepted, so CSQUAD_E2E_REPO names a disposable repository you
// trusted once (`git init /tmp/csq-e2e && cd /tmp/csq-e2e && claude`, accept,
// exit). Without it the test makes a repository in the temp dir, which works
// only where Claude Code trusts it. The task's commit is merged into the
// repository's current branch. The test removes the session and the task's
// own worktrees and branches, also when it fails.
func TestLive(t *testing.T) {
	if os.Getenv("CSQUAD_E2E_LIVE") != "1" {
		t.Skip("set CSQUAD_E2E_LIVE=1 to run against the real claude")
	}
	root, _ := filepath.EvalSymlinks(t.TempDir())
	home := filepath.Join(root, "csquad-home")
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "CLAUDE_CODE_SESSION_ID=") && !strings.HasPrefix(kv, "CSQUAD_") {
			env = append(env, kv)
		}
	}
	env = append(env, "CSQUAD_HOME="+home, "CSQUAD_TEST_MAIN=1", "CSQUAD_WAIT_INTERVAL_MS=3000", "CSQUAD_WAIT_GRACE_MS=1000")
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
	exe, _ := os.Executable()
	run := func(stdin string, args ...string) result {
		cmd := exec.Command(exe, args...)
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

	r := run("Create a file hello.txt containing the single line hi, commit it, and finish with a one-line result. Do nothing else.",
		"launch", "--title", "csquad live test", "--cwd", repo, "--", "--model", "haiku")
	m := launchedRe.FindStringSubmatch(r.stdout)
	if r.code != 0 || m == nil {
		t.Fatalf("launch failed (an untrusted repository? see CSQUAD_E2E_REPO)")
	}
	n, session := m[1], m[3]
	ours := func(name string) bool {
		return strings.HasPrefix(name, "worktree-t"+n+"-") || strings.HasPrefix(name, "task/t"+n+"-") || strings.HasPrefix(filepath.Base(name), "t"+n+"-")
	}
	t.Cleanup(func() {
		if out, err := exec.Command("claude", "agents", "--json", "--all").Output(); err == nil && strings.Contains(string(out), session) {
			exec.Command("claude", "stop", session).Run()
			exec.Command("claude", "rm", session).Run()
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

	r = run("", "wait", "T"+n, "--timeout", "10")
	if !regexp.MustCompile(`· (done|idle)\n$`).MatchString(r.stdout) {
		t.Fatalf("wait: %q", r.stdout)
	}
	run("", "status")
	run("", "peek", "T"+n, "-n", "3")
	branch := ""
	for _, b := range strings.Fields(git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")) {
		if ours(b) {
			branch = b
		}
	}
	if branch == "" {
		t.Fatalf("no branch named for T%s: %s", n, git(repo, "branch", "-a"))
	}
	git(repo, "merge", "-q", "--ff-only", branch)
	if b, err := os.ReadFile(filepath.Join(repo, "hello.txt")); err != nil || strings.TrimSpace(string(b)) != "hi" {
		t.Fatalf("hello.txt: %q %v", b, err)
	}
	if r := run("", "finish", "T"+n); r.code != 0 {
		t.Fatalf("finish failed")
	}
	out, _ := exec.Command("claude", "agents", "--json", "--all").Output()
	var list []map[string]any
	json.Unmarshal(out, &list)
	for _, s := range list {
		if s["id"] == session {
			t.Errorf("session %s still listed", session)
		}
	}
	for _, b := range strings.Fields(git(repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/")) {
		if ours(b) {
			t.Errorf("branch %s left", b)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "settings", "T"+n+".json")); err == nil {
		t.Error("settings file left")
	}
}
