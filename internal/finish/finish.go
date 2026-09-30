// Package finish removes what a finished task left behind: its session, the
// worktrees it worked in and the branches it created, but only once all of
// that work is merged. Worktrees are optional: a worker that edited the task
// directory in place, on the target branch or on its own branch, is handled
// too.
package finish

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/ShunL12324/c-squad/internal/claude"
)

// Worktree is a git worktree, other than the main checkout, the session used.
type Worktree struct {
	Path   string
	Branch string
	// Exists is false when the worktree was already removed.
	Exists bool
}

// Plan is what finishing a task will remove, and why it cannot yet.
type Plan struct {
	// Root is the main checkout of the task's repository, empty outside git.
	Root string
	// Target is the branch the work must be merged into.
	Target    string
	Worktrees []Worktree
	// Branches are branches the session worked on, other than Target, that
	// finishing deletes. Worktree branches are included.
	Branches []string
	// Blockers explain why finishing must not proceed yet.
	Blockers []string
	// Notes are harmless observations shown with the plan.
	Notes []string
	// Verified means the plan knows where the session worked: the task is in
	// git and its transcript was read. Only then may unpushed commits be
	// discarded when the session is removed.
	Verified bool
}

// Build inspects the task directory and the session's trail and decides what
// finishing removes. into overrides the target branch, which defaults to the
// branch checked out in the main checkout.
func Build(dir string, trail claude.Trail, into string) (Plan, error) {
	var p Plan
	if len(trail.Locations) == 0 {
		p.Notes = append(p.Notes, "no transcript found; the session is removed only if it holds no unpushed commits")
	}
	top, err := git(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		p.Notes = append(p.Notes, "not a git repository; only the session is removed")
		return p, nil
	}
	worktrees, err := listWorktrees(top)
	if err != nil {
		return p, err
	}
	// The first entry of `git worktree list` is always the main checkout, even
	// when dir is inside a linked worktree.
	p.Root = worktrees[0].Path
	p.Target = into
	if p.Target == "" {
		if p.Target, err = git(p.Root, "symbolic-ref", "--quiet", "--short", "HEAD"); err != nil {
			return p, errors.New("the main checkout has a detached HEAD; pass --into BRANCH")
		}
	}
	if _, err := git(p.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+p.Target); err != nil {
		return p, fmt.Errorf("target branch %s does not exist", p.Target)
	}
	for _, loc := range trail.Locations {
		wt, linked := owner(worktrees, loc.Cwd)
		switch {
		case linked:
			if !slices.ContainsFunc(p.Worktrees, func(w Worktree) bool { return w.Path == wt.Path }) {
				p.Worktrees = append(p.Worktrees, Worktree{Path: wt.Path, Branch: wt.Branch, Exists: true})
			}
			p.addBranch(wt.Branch)
		case within(loc.Cwd, p.Root):
			// Worked in place in the main checkout.
			p.addBranch(loc.Branch)
		case strings.Contains(loc.Cwd, string(filepath.Separator)+".claude"+string(filepath.Separator)+"worktrees"+string(filepath.Separator)):
			// A worktree of this session that is already gone; its branch
			// may still exist.
			p.addBranch(loc.Branch)
		}
	}
	// Branches created inside one shell command never show up as a location.
	for _, b := range trail.Created {
		p.addBranch(b)
	}
	for _, b := range p.Branches {
		p.check(b)
	}
	p.Verified = len(trail.Locations) > 0
	for i, w := range p.Worktrees {
		if _, err := os.Stat(w.Path); err != nil {
			// Deleted by hand but still registered; prune forgets it.
			p.Worktrees[i].Exists = false
			continue
		}
		if status, err := git(w.Path, "status", "--porcelain"); err != nil {
			p.Blockers = append(p.Blockers, fmt.Sprintf("cannot read worktree %s: %v", w.Path, err))
		} else if status != "" {
			p.Blockers = append(p.Blockers, fmt.Sprintf("worktree %s has uncommitted or untracked files:\n%s", w.Path, indent(status)))
		}
	}
	if status, err := git(p.Root, "status", "--porcelain", "--untracked-files=no"); err == nil && status != "" {
		p.Notes = append(p.Notes, "the main checkout has uncommitted changes; finish leaves them alone")
	}
	return p, nil
}

// addBranch records a branch to delete, ignoring the target, detached
// checkouts and branches that no longer exist.
func (p *Plan) addBranch(b string) {
	if b == "" || b == "HEAD" || b == p.Target || slices.Contains(p.Branches, b) {
		return
	}
	if _, err := git(p.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err != nil {
		return
	}
	p.Branches = append(p.Branches, b)
}

func (p *Plan) check(branch string) {
	if _, err := git(p.Root, "merge-base", "--is-ancestor", branch, p.Target); err == nil {
		return
	}
	count, _ := git(p.Root, "rev-list", "--count", p.Target+".."+branch)
	p.Blockers = append(p.Blockers, fmt.Sprintf("branch %s has %s commit(s) not in %s; merge it first", branch, count, p.Target))
}

// Session is the part of Claude Code that Run needs; tests replace it.
type Session struct {
	// Live reports whether the session still exists.
	Live bool
	// Remove deletes it. With discard set it confirms the discard of commits
	// that exist on no remote; Run sets it only when the plan verified them.
	Remove func(discard bool) error
}

// Run removes the session, then any worktree and branch the plan lists that
// is still present. It must only be called on a plan without blockers.
func Run(p Plan, s Session) error {
	if len(p.Blockers) > 0 {
		return errors.New("plan has blockers")
	}
	if s.Live {
		// Every branch the trail found is merged, so discarding "unpushed"
		// commits loses nothing. Without a verified trail (outside git, or no
		// transcript), let Claude Code's own refusal protect the work.
		if err := s.Remove(p.Verified); err != nil {
			return err
		}
	}
	if p.Root == "" {
		return nil
	}
	var errs []error
	for _, w := range p.Worktrees {
		if _, err := os.Stat(w.Path); err != nil {
			continue // removed with the session
		}
		// A worktree Claude Code created stays locked while it is in use.
		_, _ = git(p.Root, "worktree", "unlock", w.Path)
		if _, err := git(p.Root, "worktree", "remove", w.Path); err != nil {
			errs = append(errs, err)
		}
	}
	_, _ = git(p.Root, "worktree", "prune")
	checkedOut := map[string]bool{}
	if worktrees, err := listWorktrees(p.Root); err == nil {
		for _, w := range worktrees {
			checkedOut[w.Branch] = true
		}
	}
	for _, b := range p.Branches {
		if _, err := git(p.Root, "rev-parse", "--verify", "--quiet", "refs/heads/"+b); err != nil || checkedOut[b] {
			continue
		}
		// Build verified the branch is contained in the target; -d would
		// compare with HEAD instead, which may be a different branch.
		if _, err := git(p.Root, "branch", "-D", b); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

type worktree struct{ Path, Branch string }

func listWorktrees(dir string) ([]worktree, error) {
	out, err := git(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, err
	}
	var list []worktree
	for line := range strings.SplitSeq(out, "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			list = append(list, worktree{Path: strings.TrimPrefix(line, "worktree ")})
		case strings.HasPrefix(line, "branch ") && len(list) > 0:
			list[len(list)-1].Branch = strings.TrimPrefix(line, "branch refs/heads/")
		}
	}
	if len(list) == 0 {
		return nil, errors.New("git worktree list returned nothing")
	}
	return list, nil
}

// owner returns the linked worktree (not the main checkout) containing dir.
// Linked worktrees can live inside the main checkout, so the longest match wins.
func owner(worktrees []worktree, dir string) (worktree, bool) {
	best, found := worktree{}, false
	for _, w := range worktrees[1:] {
		if within(dir, w.Path) && len(w.Path) > len(best.Path) {
			best, found = w, true
		}
	}
	return best, found
}

func within(dir, root string) bool {
	return dir == root || strings.HasPrefix(dir, root+string(filepath.Separator))
}

func git(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

func indent(s string) string {
	return "    " + strings.ReplaceAll(s, "\n", "\n    ")
}
