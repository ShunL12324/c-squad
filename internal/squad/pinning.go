package squad

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/term"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/pin"
	"github.com/ShunL12324/c-squad/internal/update"
)

// ErrWrongBuild refuses a write to a pinned team from any other csquad build.
// Each team is written by exactly one build: the one recorded in its pin.
var ErrWrongBuild = errors.New("this csquad build is not the one the team is pinned to")

// selfSHA256 is replaceable in tests, whose binary is a Go test executable.
var selfSHA256 = pin.SelfSHA256

// installedExecutable prefers a PATH entry for this exact executable, so
// package-manager symlinks remain valid when their versioned target changes.
// It never selects a different csquad installation merely because it is first
// on PATH.
func installedExecutable() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", err
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

// checkWriter is the per-transaction self check: on a pinned team the running
// image must hash to the pin. The path is not compared; identical bytes are the
// same build wherever they run from. Only a re-pin's transition transaction may
// pass with a different hash.
func (st *Store) checkWriter(s *State) error {
	p, pinned := teamPin(s)
	if !pinned || st.transition {
		return nil
	}
	sha, err := selfSHA256()
	if err != nil {
		return fmt.Errorf("hash the running csquad: %w", err)
	}
	if sha != p.SHA256 {
		return fmt.Errorf("%w (team %s uses csquad %s, %s…; this is %s, %s…); %s", ErrWrongBuild, s.ID, p.Version, p.SHA256[:12], buildinfo.Version, sha[:12], pinHint(s))
	}
	return nil
}

// isPinnedBuild reports whether the running build may drive the team's runtime
// and delivery. On an unpinned team every build may, as before pinning.
func isPinnedBuild(s *State) bool {
	p, pinned := teamPin(s)
	if !pinned {
		return true
	}
	sha, err := selfSHA256()
	return err == nil && sha == p.SHA256
}

func pinHint(s *State) string {
	return "run it through the team's own build (any csquad command forwards there), or run: csquad repin " + s.ID
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
	if !pinned {
		return nil
	}
	return fmt.Errorf("team %s still uses a private csquad copy; from an outside terminal run: csquad stop %s, then csquad resume %s", s.ID, s.ID, s.ID)
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
	question := fmt.Sprintf("team %s is pinned to csquad %s (%s…); this is csquad %s (%s…), and their order cannot be determined. Pin the team to this build? [y/N] ", s.ID, p.Version, p.SHA256[:12], buildinfo.Version, sha[:12])
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

// transitionExecutable retires a legacy private copy when a team resumes.
// The one transition transaction may be written by the installed build; once
// the ordinary path is recorded, the existing unpinned-team rules apply.
func (st *Store) transitionExecutable(path string) error {
	st.transition = true
	defer func() { st.transition = false }()
	return st.update(func(s *State) error {
		s.Executable = path
		return nil
	})
}

// adoptInstalledExecutable is used by lifecycle commands that retire a
// legacy pin. They hold team-lifecycle before changing the ledger.
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
	if s.Executable != path {
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
