package squad

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GitState describes the repository a member's own working directory sits in,
// which in a cross-repository task is not the repository its task is bound to.
type GitState struct {
	Branch    string `json:"branch,omitempty"`
	Commit    string `json:"commit,omitempty"`
	GitDir    string `json:"git_dir,omitempty"`
	CommonDir string `json:"common_dir,omitempty"`
	Worktree  bool   `json:"worktree"`
}

// A directory that is not a repository must not spawn Git on every tick, and a
// relocated worktree is invisible to the HEAD check below, so both are retried
// on this interval alone.
const gitBackstop = 30 * time.Second

type gitEntry struct {
	state   GitState
	ok      bool
	head    string
	mod     time.Time
	size    int64
	checked time.Time
}

var (
	gitMu    sync.Mutex
	gitCache = map[string]*gitEntry{}
)

// memberGit resolves dir's Git state, spawning Git only when the answer can
// actually have changed. panelSnapshot runs once a second in every panel pane of
// every session, so a per-tick Git process would be multiplied by both.
func memberGit(dir string) (GitState, bool) {
	if dir == "" {
		return GitState{}, false
	}
	gitMu.Lock()
	defer gitMu.Unlock()
	if entry, seen := gitCache[dir]; seen && !entry.stale(time.Now()) {
		return entry.state, entry.ok
	}
	entry := resolveGit(dir)
	gitCache[dir] = entry
	return entry.state, entry.ok
}

// stale costs one stat and no process. HEAD is an exact change signal for
// everything the card displays: checkout rewrites it, and a commit on an
// attached branch rewrites refs/heads/<branch> instead - which cannot change the
// branch name being displayed either. A detached HEAD holds the raw commit, so
// the short SHA on the card moves with it.
func (e *gitEntry) stale(now time.Time) bool {
	if now.Sub(e.checked) >= gitBackstop {
		return true
	}
	if !e.ok {
		return false
	}
	info, err := os.Stat(e.head)
	if err != nil {
		return true
	}
	return !info.ModTime().Equal(e.mod) || info.Size() != e.size
}

// resolveGit reads repository state without touching the index: rev-parse and
// symbolic-ref take no locks, so they cannot disturb an agent working in dir.
func resolveGit(dir string) *gitEntry {
	entry := &gitEntry{checked: time.Now()}
	// repoIdentity is the resolver the cross-repository warnings already use.
	// A second one could disagree with it about which repository dir belongs to.
	common, err := repoIdentity(dir)
	if err != nil {
		return entry
	}
	state := GitState{CommonDir: common}
	out, err := runGit(dir, "rev-parse", "--path-format=absolute", "--absolute-git-dir", "--abbrev-ref", "HEAD")
	fields := strings.Split(out, "\n")
	switch {
	case err != nil || len(fields) < 2:
		// An unborn HEAD has no commit to resolve; the branch still exists.
		if state.GitDir, state.Branch = unbornHead(dir); state.GitDir == "" {
			return entry
		}
	case fields[1] == "HEAD":
		// --abbrev-ref stays in effect for every later revision, so the short
		// commit of a detached HEAD cannot come from the call above.
		state.GitDir = fields[0]
		if sha, shaErr := runGit(dir, "rev-parse", "--short", "HEAD"); shaErr == nil {
			state.Commit = sha
		}
	default:
		state.GitDir, state.Branch = fields[0], fields[1]
	}
	// One repository, many checkouts: a linked worktree keeps its own HEAD but
	// shares the common directory, which is exactly how T32 tells them apart.
	state.Worktree = !sameDir(state.GitDir, common)
	entry.state, entry.ok, entry.head = state, true, filepath.Join(state.GitDir, "HEAD")
	if info, statErr := os.Stat(entry.head); statErr == nil {
		entry.mod, entry.size = info.ModTime(), info.Size()
	}
	return entry
}

func unbornHead(dir string) (string, string) {
	gitDir, err := runGit(dir, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
	if err != nil {
		return "", ""
	}
	branch, err := runGit(dir, "symbolic-ref", "--short", "-q", "HEAD")
	if err != nil {
		return gitDir, ""
	}
	return gitDir, branch
}

// sameDir compares directories rather than their spelling, so a symlinked path
// does not read as a linked worktree.
func sameDir(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	left, err := os.Stat(a)
	right, otherErr := os.Stat(b)
	return err == nil && otherErr == nil && os.SameFile(left, right)
}
