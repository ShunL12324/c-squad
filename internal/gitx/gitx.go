// Package gitx answers the git questions finish needs: which worktrees and
// branches a task made, whether they are merged, whether a worktree is clean,
// and removes them.
package gitx

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Git runs git commands with an environment.
type Git struct {
	Env []string
}

// Run runs git -C dir args and returns its trimmed stdout.
func (g Git) Run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = g.Env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return strings.TrimSpace(out.String()), fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return strings.TrimSpace(out.String()), nil
}

// TopLevel is the root of the worktree holding dir, or "" outside git.
func (g Git) TopLevel(dir string) string {
	out, err := g.Run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return ""
	}
	return filepath.FromSlash(out)
}

// Worktree is one entry of `git worktree list --porcelain`.
type Worktree struct {
	Path     string
	Head     string
	Branch   string // short name; "" when detached
	Detached bool
	Locked   bool
	Bare     bool
}

// Worktrees lists the worktrees of the repository holding dir; the first is
// the main checkout.
func (g Git) Worktrees(dir string) ([]Worktree, error) {
	out, err := g.Run(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []Worktree
	var cur *Worktree
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "worktree":
			list = append(list, Worktree{Path: filepath.FromSlash(val)})
			cur = &list[len(list)-1]
		case "HEAD":
			if cur != nil {
				cur.Head = val
			}
		case "branch":
			if cur != nil {
				cur.Branch = strings.TrimPrefix(val, "refs/heads/")
			}
		case "detached":
			if cur != nil {
				cur.Detached = true
			}
		case "locked":
			if cur != nil {
				cur.Locked = true
			}
		case "bare":
			if cur != nil {
				cur.Bare = true
			}
		}
	}
	return list, nil
}

// Branches lists the local branches.
func (g Git) Branches(dir string) ([]string, error) {
	out, err := g.Run(dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return nil, err
	}
	if out == "" {
		return nil, nil
	}
	return strings.Split(out, "\n"), nil
}

// IsAncestor reports whether commit a is an ancestor of (merged into) b.
func (g Git) IsAncestor(dir, a, b string) (bool, error) {
	cmd := exec.Command("git", "-C", dir, "merge-base", "--is-ancestor", a, b)
	cmd.Env = g.Env
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %s", a, b, strings.TrimSpace(errb.String()))
}

// Commit is a commit of a branch that is not reachable from a target.
type Commit struct {
	Hash    string // abbreviated
	Subject string
	InPatch bool // a commit with the same patch is in the target (cherry-picked or rebased)
}

// Merged reports whether head's work is in target: head is an ancestor of
// target, or every commit of head not reachable from target has a
// patch-equivalent commit in target (byPatch). commits are head's commits
// not reachable from target, oldest first. A merge commit is never
// patch-equivalent, and a conflict-resolved cherry-pick changes the patch:
// both stay unmerged.
func (g Git) Merged(dir, head, target string) (merged, byPatch bool, commits []Commit, err error) {
	if ok, err := g.IsAncestor(dir, head, target); err != nil || ok {
		return ok, false, nil, err
	}
	// The right side of target...head is what git cherry target head lists;
	// --cherry-mark marks the patch-equivalent ones "=" and, unlike git
	// cherry, keeps merge commits (">").
	out, err := g.Run(dir, "log", "--cherry-mark", "--right-only", "--reverse", "--format=%m %h %s", target+"..."+head, "--")
	if err != nil {
		return false, false, nil, err
	}
	byPatch = true
	for _, line := range strings.Split(out, "\n") {
		mark, rest, _ := strings.Cut(strings.TrimRight(line, "\r"), " ")
		if mark == "" {
			continue
		}
		hash, subject, _ := strings.Cut(rest, " ")
		c := Commit{Hash: hash, Subject: subject, InPatch: mark == "="}
		byPatch = byPatch && c.InPatch
		commits = append(commits, c)
	}
	byPatch = byPatch && len(commits) > 0
	return byPatch, byPatch, commits, nil
}

// Dirty returns `git status --porcelain` of a worktree ("" when clean).
func (g Git) Dirty(dir string) (string, error) {
	return g.Run(dir, "status", "--porcelain")
}

// Changes lists a worktree's uncommitted changes: the tracked paths that
// differ from HEAD (staged or not), and every untracked file (not
// directories).
func (g Git) Changes(dir string) (tracked, untracked []string, err error) {
	cmd := exec.Command("git", "-C", dir, "status", "--porcelain", "-z", "--untracked-files=all")
	cmd.Env = g.Env
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, nil, fmt.Errorf("git status: %s", strings.TrimSpace(errb.String()))
	}
	entries := strings.Split(out.String(), "\x00")
	for i := 0; i < len(entries); i++ {
		e := entries[i]
		if len(e) < 4 {
			continue
		}
		xy, path := e[:2], e[3:]
		switch xy {
		case "??":
			untracked = append(untracked, path)
		case "!!":
		default:
			tracked = append(tracked, path)
			if xy[0] == 'R' || xy[0] == 'C' {
				i++ // the next entry is the source path
			}
		}
	}
	return tracked, untracked, nil
}

// RemoveWorktree unlocks (Claude Code locks the worktrees it creates) and
// removes a worktree, without --force.
func (g Git) RemoveWorktree(repo string, w Worktree) error {
	if w.Locked {
		if _, err := g.Run(repo, "worktree", "unlock", w.Path); err != nil {
			return err
		}
	}
	_, err := g.Run(repo, "worktree", "remove", w.Path)
	return err
}

// Prune drops administrative data of worktrees that are gone.
func (g Git) Prune(repo string) error {
	_, err := g.Run(repo, "worktree", "prune")
	return err
}

// DeleteBranch deletes a merged branch (git branch -d, never -D).
func (g Git) DeleteBranch(repo, branch string) error {
	_, err := g.Run(repo, "branch", "-d", branch)
	return err
}

// DeleteMerged deletes a branch merged into target. git branch -d judges
// "merged" against HEAD, so for a target that is not checked out the ref is
// deleted only while it still points at the commit verified merged.
func (g Git) DeleteMerged(repo, branch, target string) error {
	err := g.DeleteBranch(repo, branch)
	if err == nil || target == "" {
		return err
	}
	sha, rerr := g.Run(repo, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if rerr != nil {
		return err
	}
	if ok, aerr := g.IsAncestor(repo, sha, "refs/heads/"+target); aerr != nil || !ok {
		return err
	}
	_, err = g.Run(repo, "update-ref", "-d", "refs/heads/"+branch, sha)
	return err
}

// DeleteByPatch deletes a branch whose commits were verified in the target
// by patch (Merged said byPatch), while it still points at sha, the commit
// verified. git branch -d refuses such a branch, so this is -D.
func (g Git) DeleteByPatch(repo, branch, sha string) error {
	cur, err := g.Run(repo, "rev-parse", "--verify", "-q", "refs/heads/"+branch)
	if err != nil {
		return err
	}
	if cur != sha {
		return fmt.Errorf("branch %s moved since it was verified merged by patch", branch)
	}
	_, err = g.Run(repo, "branch", "-D", branch)
	return err
}

// Refs are what a task made, by csquad's naming: worktrees named t12-slug,
// branches worktree-t12-slug and task/t12-slug.
type Refs struct {
	Worktrees []Worktree
	Branches  []string
	// Ignored are refs so named that are older than the task ("worktree
	// PATH", "branch NAME"): another task with the same number made them
	// (csquad 1.x reused numbers). They are never checked or removed.
	Ignored []string
}

// Slack is how much older than the task a ref may look and still be the
// task's: reflogs count whole seconds.
const Slack = 5 * time.Second

// TaskRefs finds the worktrees and branches named for task id, and the
// branches of worktrees so named, made after since (zero: any time). A
// branch dates from its oldest reflog entry, a worktree from its .git file;
// a ref whose age is unknown counts as the task's.
func (g Git) TaskRefs(repo string, id int, since time.Time) (Refs, error) {
	all, err := g.namedRefs(repo, id)
	if err != nil || since.IsZero() {
		return all, err
	}
	since = since.Add(-Slack)
	var r Refs
	old := map[string]bool{}
	for _, b := range all.Branches {
		if at, ok := g.branchCreated(repo, b); ok && at.Before(since) {
			old[b] = true
			r.Ignored = append(r.Ignored, "branch "+b)
			continue
		}
		r.Branches = append(r.Branches, b)
	}
	for _, w := range all.Worktrees {
		st, err := os.Stat(filepath.Join(w.Path, ".git"))
		if err == nil && st.ModTime().Before(since) || w.Branch != "" && old[w.Branch] {
			r.Ignored = append(r.Ignored, "worktree "+w.Path)
			continue
		}
		r.Worktrees = append(r.Worktrees, w)
	}
	return r, nil
}

// branchCreated is the time of the branch's oldest reflog entry.
func (g Git) branchCreated(repo, branch string) (time.Time, bool) {
	out, err := g.Run(repo, "reflog", "show", "--date=unix", "--format=%gd", "refs/heads/"+branch, "--")
	if err != nil || out == "" {
		return time.Time{}, false
	}
	lines := strings.Split(out, "\n")
	last := lines[len(lines)-1]
	i, j := strings.LastIndex(last, "@{"), strings.LastIndex(last, "}")
	if i < 0 || j < i {
		return time.Time{}, false
	}
	sec, err := strconv.ParseInt(last[i+2:j], 10, 64)
	if err != nil {
		return time.Time{}, false
	}
	return time.Unix(sec, 0), true
}

// namedRefs finds the worktrees and branches named for task id, and the
// branches of worktrees so named.
func (g Git) namedRefs(repo string, id int) (Refs, error) {
	var r Refs
	prefix := fmt.Sprintf("t%d-", id)
	wts, err := g.Worktrees(repo)
	if err != nil {
		return r, err
	}
	branches, err := g.Branches(repo)
	if err != nil {
		return r, err
	}
	ours := map[string]bool{}
	for _, b := range branches {
		if strings.HasPrefix(b, "worktree-"+prefix) || strings.HasPrefix(b, "task/"+prefix) {
			ours[b] = true
			r.Branches = append(r.Branches, b)
		}
	}
	for i, w := range wts {
		if i == 0 || w.Bare {
			continue // the main checkout is never a task's
		}
		named := strings.HasPrefix(filepath.Base(w.Path), prefix)
		switch {
		case w.Branch != "" && (ours[w.Branch] || named):
			r.Worktrees = append(r.Worktrees, w)
			if !ours[w.Branch] {
				ours[w.Branch] = true
				r.Branches = append(r.Branches, w.Branch)
			}
		case named:
			if _, err := os.Stat(w.Path); err == nil {
				r.Worktrees = append(r.Worktrees, w) // detached
			}
		}
	}
	return r, nil
}
