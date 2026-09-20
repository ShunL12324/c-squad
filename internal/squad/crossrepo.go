package squad

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Warnings and notices go to stderr so stdout stays a parseable JSON document.
var notices io.Writer = os.Stderr

func warn(text string) {
	_, _ = fmt.Fprintln(notices, "csquad: warning: "+text)
}
func printNotice(lines []string) {
	for _, text := range lines {
		_, _ = fmt.Fprintln(notices, "csquad: "+text)
	}
}

// repoIdentity returns the absolute common Git directory of dir. A repository and
// every worktree cut from it share one common directory, so this identifies the
// repository rather than the checkout.
func repoIdentity(dir string) (string, error) {
	if dir == "" {
		return "", errors.New("no directory given")
	}
	return runGit(dir, "rev-parse", "--path-format=absolute", "--git-common-dir")
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}

// codeTaskBinding states where a code task actually lives. task create --code
// always binds to the team repository, which members frequently assume follows
// their own --cwd instead.
func codeTaskBinding(s *State, t *Task) string {
	return fmt.Sprintf("task %s is bound to repository %s (worktree %s, branch %s, base %s, target branch %s); --code worktrees are always created in the team repository and do not inherit a member --cwd",
		t.ID, s.Root, t.Workspace, t.Branch, short(t.Base), t.Target)
}

// crossRepoNotice reports that a member cannot submit work for this code task
// from its own startup directory. An empty result means the directories share a
// repository, or that there is nothing to compare. The comparison uses the team
// repository rather than the worktree, so it also holds at create time, before
// the worktree exists.
func crossRepoNotice(s *State, t *Task, m *Member) string {
	if t == nil || m == nil || t.Workspace == "" || m.Cwd == "" {
		return ""
	}
	bound, err := repoIdentity(s.Root)
	if err != nil {
		return ""
	}
	member, err := repoIdentity(m.Cwd)
	if err != nil {
		return fmt.Sprintf("member %s starts in %s, which is not a Git repository, but %s. Commits made outside the task worktree cannot be submitted with task submit.",
			m.ID, m.Cwd, codeTaskBinding(s, t))
	}
	if member == bound {
		return ""
	}
	return fmt.Sprintf("member %s starts in %s, a different Git repository, but %s. A commit made in %s cannot be submitted with task submit; master can close such a task with task close-external.",
		m.ID, m.Cwd, codeTaskBinding(s, t), m.Cwd)
}

// noticeCrossRepo emits a cross-repository warning and records it in the ledger.
// It never fails the command: the binding is legal, only surprising.
func noticeCrossRepo(s *State, actor string, t *Task, m *Member) string {
	notice := crossRepoNotice(s, t, m)
	if notice == "" {
		return ""
	}
	warn(notice)
	s.event(actor, "cross_repo_member", t.ID+" "+m.ID+" "+m.Cwd)
	return notice
}

// noticeCrossRepoTeam warns about every existing member that could not submit work
// for this new code task, in a stable order and with one ledger event.
func noticeCrossRepoTeam(s *State, actor string, t *Task) {
	ids := make([]string, 0, len(s.Members))
	for id, m := range s.Members {
		if id == "master" || m.State == MemberStateRemoved {
			continue
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	affected := []string{}
	for _, id := range ids {
		if text := crossRepoNotice(s, t, s.Members[id]); text != "" {
			warn(text)
			affected = append(affected, id)
		}
	}
	if len(affected) > 0 {
		s.event(actor, "cross_repo_member", t.ID+" "+strings.Join(affected, ","))
	}
}

// closeExternal records a master decision to close a code task on a commit held
// in a repository the team does not own. It runs no Git write command: nothing is
// fetched, imported or merged, and MergeCommit stays empty so no surface can
// present the result as a merge.
func closeExternal(s *State, actor string, t *Task, o options) error {
	if t.Workspace == "" {
		return errors.New("close-external applies to code tasks; approve tasks without a workspace normally")
	}
	// A resolvable candidate means the normal review and fast-forward merge path
	// still works. Require an audited task reopen first so this can never become a
	// quiet substitute for evidence and merge.
	if t.Candidate != "" {
		return fmt.Errorf("task %s still has candidate %s in its own worktree; use task reopen first if it must be abandoned", t.ID, short(t.Candidate))
	}
	if strings.TrimSpace(o["reason"]) == "" {
		return errors.New("--reason required: record why this task is closed without a merge")
	}
	if strings.TrimSpace(o["sha"]) == "" {
		return errors.New("--sha required: record the real commit this closure relies on")
	}
	if strings.TrimSpace(o["repo"]) == "" {
		return errors.New("--repo required: name the repository holding the commit")
	}
	repo, err := filepath.Abs(o["repo"])
	if err != nil {
		return fmt.Errorf("external repository: %w", err)
	}
	info, err := os.Stat(repo)
	if err != nil {
		return fmt.Errorf("external repository %q: %w", repo, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("external repository %q is not a directory", repo)
	}
	gitDir, err := repoIdentity(repo)
	if err != nil {
		return fmt.Errorf("external repository %q is not a Git repository: %w", repo, err)
	}
	own, err := repoIdentity(s.Root)
	if err != nil {
		return err
	}
	// Work inside the team repository must go through evidence, approval and the
	// fast-forward-only merge. This entry point exists only for work the team
	// repository genuinely cannot reach.
	if gitDir == own {
		return fmt.Errorf("%s is the team repository %s; submit, review and merge it normally instead of closing it externally", repo, s.Root)
	}
	sha, err := git(repo, "rev-parse", "--verify", o["sha"]+"^{commit}")
	if err != nil {
		return fmt.Errorf("commit %q does not resolve in external repository %s: %w", o["sha"], repo, err)
	}
	top, err := git(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		top = repo
	}
	subject, err := git(repo, "log", "-1", "--format=%s", sha)
	if err != nil {
		subject = ""
	}
	closure := &ExternalClosure{
		Repo:    top,
		GitDir:  gitDir,
		SHA:     sha,
		Subject: subject,
		Reason:  o["reason"],
		Summary: o["summary"],
		Limits: fmt.Sprintf("commit %s was verified only to exist in %s; no C-Squad review or test evidence is bound to it; no merge into %s branch %s was performed; the unmerged task worktree %s (branch %s) is left in place.",
			short(sha), top, s.Root, t.Target, t.Workspace, t.Branch),
		By:        actor,
		At:        now(),
		Workspace: t.Workspace,
		Branch:    t.Branch,
	}
	t.ExternalClosure = closure
	t.Approval = nil
	t.State = TaskPhaseDone
	s.event(actor, "closed_external", fmt.Sprintf("%s %s@%s %s", t.ID, short(sha), top, closure.Reason))
	for _, id := range t.Participants {
		s.message(actor, id, t.ID, "Task "+t.ID+" closed on external commit "+short(sha)+" in "+top+" without a merge. Reason: "+closure.Reason+" "+closure.Limits, "")
	}
	return nil
}

// describeExternalClosure renders the audit record for human-facing surfaces so an
// externally closed task is never read as a merged one.
func describeExternalClosure(c *ExternalClosure) []string {
	if c == nil {
		return nil
	}
	head := "Closed externally by " + c.By + " on " + c.At + " — NOT merged"
	lines := []string{head, "Commit: " + short(c.SHA) + " in " + c.Repo}
	if c.Subject != "" {
		lines = append(lines, "Subject: "+c.Subject)
	}
	lines = append(lines, "Reason: "+c.Reason)
	if c.Summary != "" {
		lines = append(lines, "Summary: "+c.Summary)
	}
	return append(lines, "Verification limits: "+c.Limits)
}
