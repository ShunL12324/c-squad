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
	"strings"
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

// Dirty returns `git status --porcelain` of a worktree ("" when clean).
func (g Git) Dirty(dir string) (string, error) {
	return g.Run(dir, "status", "--porcelain")
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

// Refs are what a task made, by csquad's naming: worktrees named t12-slug,
// branches worktree-t12-slug and task/t12-slug.
type Refs struct {
	Worktrees []Worktree
	Branches  []string
}

// TaskRefs finds the worktrees and branches named for task id, and the
// branches of worktrees so named.
func (g Git) TaskRefs(repo string, id int) (Refs, error) {
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
