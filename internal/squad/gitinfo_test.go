package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/teamui"
)

// resetGitCache keeps one test's resolutions out of the next one's counts.
func resetGitCache(t *testing.T) {
	t.Helper()
	gitMu.Lock()
	gitCache = map[string]*gitEntry{}
	gitMu.Unlock()
	t.Cleanup(func() {
		gitMu.Lock()
		gitCache = map[string]*gitEntry{}
		gitMu.Unlock()
	})
}

func TestMemberGitReadsTheDirectoryItIsGiven(t *testing.T) {
	resetGitCache(t)
	root := t.TempDir()
	initRepo(t, root)
	commitFile(t, root, "first.txt", "one")
	if _, e := git(root, "checkout", "-b", "feature/example"); e != nil {
		t.Fatal(e)
	}
	nested := filepath.Join(root, "pkg", "deep")
	if e := os.MkdirAll(nested, 0700); e != nil {
		t.Fatal(e)
	}
	linked := filepath.Join(t.TempDir(), "wt")
	if _, e := git(root, "worktree", "add", "-b", "csquad/example/T12", linked); e != nil {
		t.Fatal(e)
	}
	detached := t.TempDir()
	initRepo(t, detached)
	sha := commitFile(t, detached, "second.txt", "two")
	if _, e := git(detached, "checkout", "--detach", sha); e != nil {
		t.Fatal(e)
	}

	for _, tt := range []struct {
		name, dir, branch string
		detachedHEAD      bool
		worktree, ok      bool
	}{
		{name: "plain repository", dir: root, branch: "feature/example", ok: true},
		{name: "subdirectory resolves upward", dir: nested, branch: "feature/example", ok: true},
		{name: "linked worktree", dir: linked, branch: "csquad/example/T12", worktree: true, ok: true},
		{name: "detached HEAD", dir: detached, detachedHEAD: true, ok: true},
		{name: "not a repository", dir: t.TempDir()},
	} {
		t.Run(tt.name, func(t *testing.T) {
			state, ok := memberGit(tt.dir)
			if ok != tt.ok {
				t.Fatalf("resolved=%v, want %v (%+v)", ok, tt.ok, state)
			}
			if !ok {
				if state.Branch != "" || state.Commit != "" {
					t.Fatalf("a directory outside Git reported %+v", state)
				}
				return
			}
			if state.Branch != tt.branch {
				t.Fatalf("branch %q, want %q", state.Branch, tt.branch)
			}
			if tt.detachedHEAD {
				if state.Commit == "" || !strings.HasPrefix(sha, state.Commit) {
					t.Fatalf("detached HEAD reported commit %q, want a prefix of %s", state.Commit, sha)
				}
			} else if state.Commit != "" {
				t.Fatalf("an attached branch reported commit %q", state.Commit)
			}
			if state.Worktree != tt.worktree {
				t.Fatalf("worktree=%v, want %v", state.Worktree, tt.worktree)
			}
		})
	}
}

// An unborn HEAD has no commit to abbreviate, which the combined rev-parse
// cannot report, so the branch has to come from symbolic-ref instead.
func TestMemberGitHandlesAnUnbornBranch(t *testing.T) {
	resetGitCache(t)
	dir := t.TempDir()
	if _, e := git(dir, "init", "-b", "main"); e != nil {
		t.Fatal(e)
	}
	state, ok := memberGit(dir)
	if !ok || state.Branch != "main" || state.Commit != "" {
		t.Fatalf("empty repository reported %+v (resolved=%v)", state, ok)
	}
}

func TestMemberGitSpawnsGitOnlyWhenTheAnswerCanHaveChanged(t *testing.T) {
	resetGitCache(t)
	repo, plain := t.TempDir(), t.TempDir()
	initRepo(t, repo)
	commitFile(t, repo, "first.txt", "one")
	absent := t.TempDir()

	var calls int
	real := runGit
	runGit = func(cwd string, args ...string) (string, error) {
		calls++
		return real(cwd, args...)
	}
	t.Cleanup(func() { runGit = real })
	initRepo(t, plain)

	// Cold: each directory resolves once. A non-repository costs its rev-parse
	// too, which is exactly why the negative result is cached.
	for _, dir := range []string{repo, plain, absent} {
		memberGit(dir)
	}
	cold := calls
	if cold == 0 {
		t.Fatal("resolution never ran")
	}
	// Ten ticks of a three-member panel. Without the cache this is 30 lookups.
	for range 10 {
		for _, dir := range []string{repo, plain, absent} {
			memberGit(dir)
		}
	}
	if calls != cold {
		t.Fatalf("panelSnapshot spawned Git %d times while nothing changed", calls-cold)
	}

	// A branch switch rewrites HEAD, so the next lookup must see it.
	if _, e := git(repo, "checkout", "-b", "feature/switched"); e != nil {
		t.Fatal(e)
	}
	// Some filesystems carry coarse timestamps; the size change alone still
	// differs here, but wait so the test does not depend on that.
	time.Sleep(10 * time.Millisecond)
	state, _ := memberGit(repo)
	if state.Branch != "feature/switched" {
		t.Fatalf("a branch switch left %q on the card", state.Branch)
	}
	switched := calls
	if switched <= cold {
		t.Fatal("the branch switch was served from cache")
	}
	for range 5 {
		for _, dir := range []string{repo, plain, absent} {
			memberGit(dir)
		}
	}
	if calls != switched {
		t.Fatalf("Git ran %d more times after the state settled again", calls-switched)
	}
}

func TestGitBackstopRefreshesWhatStatCannotSee(t *testing.T) {
	resetGitCache(t)
	dir := t.TempDir()
	if _, ok := memberGit(dir); ok {
		t.Fatal("an empty directory resolved as a repository")
	}
	gitMu.Lock()
	entry := gitCache[dir]
	if entry == nil {
		t.Fatal("the negative result was not cached")
	}
	if entry.stale(time.Now()) {
		gitMu.Unlock()
		t.Fatal("a fresh negative result asked for another lookup")
	}
	expired := entry.stale(entry.checked.Add(gitBackstop))
	gitMu.Unlock()
	if !expired {
		t.Fatal("a directory that later becomes a repository would never be noticed")
	}
	initRepo(t, dir)
	gitMu.Lock()
	gitCache[dir].checked = time.Now().Add(-gitBackstop)
	gitMu.Unlock()
	if state, ok := memberGit(dir); !ok || state.Branch != "main" {
		t.Fatalf("the backstop did not pick up the new repository: %+v", state)
	}
}

// Criterion 3, the reason the issue exists: a member working in another
// repository must show ITS branch, never the one its task is bound to.
func TestPanelSnapshotShowsTheMemberBranchNotTheTaskWorkspace(t *testing.T) {
	resetGitCache(t)
	st := testStore(t)
	team, elsewhere := t.TempDir(), t.TempDir()
	initRepo(t, team)
	commitFile(t, team, "team.txt", "team")
	initRepo(t, elsewhere)
	commitFile(t, elsewhere, "other.txt", "other")
	if _, e := git(elsewhere, "checkout", "-b", "feature/their-own"); e != nil {
		t.Fatal(e)
	}
	taskWorktree := filepath.Join(t.TempDir(), "T99")
	if _, e := git(team, "worktree", "add", "-b", "csquad/csquad/T99", taskWorktree); e != nil {
		t.Fatal(e)
	}
	must(t, st.update(func(s *State) error {
		s.Root = team
		s.Members["a"].Cwd = elsewhere
		s.Tasks["T99"] = &Task{ID: "T99", Title: "Cross repository work", Owner: "a", State: TaskPhaseInProgress,
			Workspace: taskWorktree, Branch: "csquad/csquad/T99", Target: "main"}
		return nil
	}))
	snapshot, err := st.panelSnapshot()
	must(t, err)
	var card *teamui.Member
	for i, member := range snapshot.Members {
		if member.ID == "a" {
			card = &snapshot.Members[i]
		}
	}
	if card == nil {
		t.Fatal("member a is missing from the snapshot")
	}
	if card.Branch == "csquad/csquad/T99" {
		t.Fatal("the card shows the task's branch instead of the member's own")
	}
	if card.Branch != "feature/their-own" {
		t.Fatalf("branch %q, want feature/their-own", card.Branch)
	}
	if card.Worktree {
		t.Fatal("the member's own plain repository was reported as a linked worktree")
	}
}
