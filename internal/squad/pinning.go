package squad

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/pin"
	"github.com/ShunL12324/c-squad/internal/process"
	"github.com/ShunL12324/c-squad/internal/update"
)

// ErrWrongBuild refuses writes from a different build or a replaced install.
var ErrWrongBuild = errors.New("this csquad build is not the one recorded for the team")

// selfSHA256 is replaceable in tests, whose binary is a Go test executable.
var selfSHA256 = pin.SelfSHA256

// Capture the executable's file identity at process start. The path may later
// be replaced by a package manager while this process is still running.
var runningImageInfo, runningImageErr = func() (os.FileInfo, error) {
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	return os.Stat(path)
}()

func installedImageUnchanged(path string) bool {
	if runningImageErr != nil || path == "" {
		return false
	}
	current, err := os.Stat(path)
	return err == nil && os.SameFile(runningImageInfo, current) &&
		runningImageInfo.Size() == current.Size() && runningImageInfo.ModTime().Equal(current.ModTime())
}

func temporaryExecutable(path string) bool {
	for _, part := range strings.Split(filepath.Clean(path), string(os.PathSeparator)) {
		if part == "_npx" {
			return true
		}
		if strings.HasPrefix(part, "go-build") {
			if _, err := strconv.ParseUint(strings.TrimPrefix(part, "go-build"), 10, 64); err == nil {
				return true
			}
		}
	}
	return false
}

// installedExecutable prefers a PATH entry for this exact executable, so
// package-manager symlinks remain valid when their versioned target changes.
// It never selects a different csquad installation merely because it is first
// on PATH.
func installedExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	if !strings.HasSuffix(filepath.Base(self), ".test") && temporaryExecutable(self) {
		return "", fmt.Errorf("%s is a temporary csquad installation; install csquad in a persistent location before starting or resuming a team", self)
	}
	selfInfo, err := os.Stat(self)
	if err != nil {
		return "", err
	}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		candidate := filepath.Join(dir, "csquad")
		if info, err := os.Stat(candidate); err == nil && os.SameFile(selfInfo, info) {
			return filepath.Abs(candidate)
		}
	}
	// os.Executable is absolute on supported platforms, including when the
	// command is launched through a wrapper instead of a PATH symlink.
	return self, nil
}

// teamPin reports the build a team is pinned to. Teams started before pinning
// record an ordinary install path and are unpinned.
func teamPin(s *State) (pin.Pin, bool) {
	return pin.Parse(s.Executable)
}

// checkWriter fences every ledger transaction to the team's recorded build.
// Only the lifecycle transition may write a different build.
func (st *Store) checkWriter(s *State) error {
	p, pinned := teamPin(s)
	if st.transition {
		return nil
	}
	if !pinned && s.BuildSHA256 == "" {
		return nil
	}
	sha, err := selfSHA256()
	if err != nil {
		return fmt.Errorf("hash the running csquad: %w", err)
	}
	if pinned && sha != p.SHA256 {
		return fmt.Errorf("%w (team %s uses csquad %s, %s…; this is %s, %s…); %s", ErrWrongBuild, s.ID, p.Version, p.SHA256[:12], buildinfo.Version, sha[:12], pinHint(s))
	}
	if !pinned && (sha != s.BuildSHA256 || !installedImageUnchanged(s.Executable)) {
		return fmt.Errorf("%w: team %s uses a different installed csquad build; stop and resume it from an outside terminal", ErrWrongBuild, s.ID)
	}
	return nil
}

// isTeamBuild reports whether the running build may drive the team's runtime.
func isTeamBuild(s *State) bool {
	p, pinned := teamPin(s)
	if !pinned {
		if s.BuildSHA256 == "" {
			return true
		}
		if !installedImageUnchanged(s.Executable) {
			return false
		}
		sha, err := selfSHA256()
		return err == nil && sha == s.BuildSHA256
	}
	sha, err := selfSHA256()
	return err == nil && sha == p.SHA256
}

func pinHint(s *State) string {
	return "from an outside terminal run: csquad stop " + s.ID + ", then csquad resume " + s.ID
}

// ensurePin verifies the pinned copy before csquad starts or forwards to it.
// When the copy is missing or damaged but this process is the same build, it
// is re-created from the running image: identical bytes, so no other build
// writes. Otherwise the failure names the one recovery command.
func ensurePin(s *State) error {
	p, pinned := teamPin(s)
	if !pinned {
		return nil
	}
	err := pin.Verify(p)
	if err == nil {
		return nil
	}
	if sha, e := selfSHA256(); e == nil && sha == p.SHA256 {
		if _, e = pin.Create(); e == nil && pin.Verify(p) == nil {
			return nil
		}
	}
	return fmt.Errorf("team %s is pinned to csquad %s (%s…), but its copy is unusable: %w; run: csquad repin %s", s.ID, p.Version, p.SHA256[:12], err, s.ID)
}

// forward protects a legacy team from silently launching its private copy.
// The user must stop and resume it once before normal team commands continue.
func forward(s *State) error {
	_, pinned := teamPin(s)
	if !pinned && (s.Executable == "" || s.BuildSHA256 != "") {
		if s.BuildSHA256 != "" && !isTeamBuild(s) {
			return fmt.Errorf("%w: team %s uses a different installed csquad build; from an outside terminal run: csquad stop %s, then csquad resume %s", ErrWrongBuild, s.ID, s.ID, s.ID)
		}
		return nil
	}
	return fmt.Errorf("team %s needs a one-time executable migration; from an outside terminal run: csquad stop %s, then csquad resume %s", s.ID, s.ID, s.ID)
}

func stderrIsTerminal() bool { return term.IsTerminal(os.Stderr.Fd()) }

// decideRepin applies the downgrade rule before a resume or repin moves a team
// to the running build. A known older build is refused; a build that cannot be
// ordered needs an interactive yes.
func decideRepin(s *State) error {
	p, pinned := teamPin(s)
	if !pinned {
		return nil
	}
	sha, err := selfSHA256()
	if err != nil {
		return fmt.Errorf("hash the running csquad: %w", err)
	}
	switch pin.Compare(buildinfo.Version, sha, p) {
	case pin.Same, pin.Newer:
		return nil
	case pin.Older:
		return fmt.Errorf("team %s is pinned to csquad %s; this is %s, which is older. Use that build: %s resume %s", s.ID, p.Version, buildinfo.Version, p.Path, s.ID)
	}
	question := fmt.Sprintf("team %s is pinned to csquad %s (%s…); this is csquad %s (%s…), and their order cannot be determined. Move the team to this installed build? [y/N] ", s.ID, p.Version, p.SHA256[:12], buildinfo.Version, sha[:12])
	if !confirm(question) {
		return fmt.Errorf("team %s keeps csquad %s; use that build: %s resume %s", s.ID, p.Version, p.Path, s.ID)
	}
	return nil
}

// confirm asks on an interactive terminal; without one the answer is no.
var confirm = func(question string) bool {
	if !term.IsTerminal(os.Stdin.Fd()) || !stderrIsTerminal() {
		return false
	}
	fmt.Fprint(os.Stderr, question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

// transitionExecutable records the new build after all old processes stop.
func (st *Store) transitionExecutable(path string) error {
	sha, err := selfSHA256()
	if err != nil {
		return err
	}
	st.transition = true
	defer func() { st.transition = false }()
	return st.update(func(s *State) error {
		s.Executable = path
		s.BuildSHA256 = sha
		s.Active = false
		s.Phase = TeamPhaseStopping
		return nil
	})
}

// quiesceOldBuild stops every process that might still write the old schema.
// The caller holds team-lifecycle; these locks exclude concurrent member and
// runtime launches until the new build is committed to the ledger.
func (st *Store) quiesceOldBuild(s *State) error {
	var unlocks []func()
	defer func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}()
	names := []string{"runtime-start"}
	for id := range s.Members {
		names = append(names, "member-"+id)
	}
	sort.Strings(names)
	for _, name := range names {
		unlock, err := filelock.Acquire(st.Dir, name, false)
		if err != nil {
			return err
		}
		unlocks = append(unlocks, unlock)
	}
	name := runtimeName(s)
	if _, err := tm(s, "has-session", "-t", "="+name); err == nil {
		out, err := tm(s, "display-message", "-p", "-t", "="+name+":", "#{pane_pid} #{pane_dead}")
		if err != nil {
			return err
		}
		fields := strings.Fields(out)
		if len(fields) != 2 || (fields[1] != "0" && fields[1] != "1") {
			return fmt.Errorf("cannot identify old runtime process: %q", out)
		}
		if fields[1] == "0" {
			pid, err := strconv.Atoi(fields[0])
			if err != nil || pid <= 1 {
				return fmt.Errorf("cannot identify old runtime process: %q", out)
			}
			all, err := process.Snapshot()
			if err != nil {
				return err
			}
			if identity, ok := all[pid]; ok {
				if err := process.StopTree(pid, identity.Start); err != nil {
					return fmt.Errorf("stop old runtime: %w", err)
				}
			}
		}
		if _, err := tm(s, "kill-session", "-t", "="+name); err != nil {
			if _, stillPresent := tm(s, "has-session", "-t", "="+name); stillPresent == nil {
				return err
			}
		}
	}
	ids := make([]string, 0, len(s.Members))
	for id := range s.Members {
		if id != "master" {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	ids = append(ids, "master")
	for _, id := range ids {
		if err := killMember(st, id); err != nil {
			return fmt.Errorf("stop old member %s: %w", id, err)
		}
	}
	return nil
}

// adoptInstalledExecutable is used by lifecycle commands changing the saved
// build. They hold team-lifecycle before changing the ledger.
func (st *Store) adoptInstalledExecutable(s *State) (string, error) {
	path, err := installedExecutable()
	if err != nil {
		return "", err
	}
	if _, pinned := teamPin(s); pinned {
		if err = decideRepin(s); err != nil {
			return "", err
		}
	}
	sha, err := selfSHA256()
	if err != nil {
		return "", err
	}
	if s.Executable != path || s.BuildSHA256 != sha {
		if err = st.quiesceOldBuild(s); err != nil {
			return "", fmt.Errorf("stop old csquad processes before changing builds: %w", err)
		}
		if err = st.transitionExecutable(path); err != nil {
			return "", fmt.Errorf("select installed csquad for the team: %w", err)
		}
	}
	return path, nil
}

// repinTeam migrates a legacy team to the installed executable. It can repair
// a missing private copy without creating another one. An active team is
// stopped after the transition and can then be resumed normally.
func repinTeam(st *Store, yes bool) error {
	unlock, err := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if err != nil {
		return err
	}
	defer unlock()
	s, err := st.read()
	if err != nil {
		return err
	}
	_, pinned := teamPin(s)
	if !pinned {
		return fmt.Errorf("team %s already uses the installed csquad executable", s.ID)
	}
	if s.Active && !yes && !confirm(fmt.Sprintf("team %s is running; moving it to the installed csquad will stop it. Continue? [y/N] ", s.ID)) {
		return fmt.Errorf("team %s was not changed", s.ID)
	}
	path, err := st.adoptInstalledExecutable(s)
	if err != nil {
		return err
	}
	if s.Active {
		if err = cleanupTeam(st, "stopped"); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "csquad: team %s now uses the installed executable %s; resume it with: csquad resume %s\n", s.ID, path, s.ID)
	return nil
}

// savedTeams lists the teams csquad can find for update's report, reading
// only. Teams in other projects are not discoverable, which is why pinned
// copies are never removed automatically.
func savedTeams() []update.Team {
	dirs, err := teamDirectories()
	if err != nil {
		return nil
	}
	var teams []update.Team
	for _, dir := range dirs {
		st, err := openStore(dir)
		if err != nil {
			continue
		}
		s, err := st.read()
		_ = st.DB.Close()
		if err != nil {
			continue
		}
		p, pinned := teamPin(s)
		teams = append(teams, update.Team{Name: s.ID, Active: s.Active, Pinned: pinned, Version: p.Version})
	}
	return teams
}
