package squad

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/pin"
	"github.com/charmbracelet/x/term"
)

// ErrWrongBuild refuses a write to a pinned team from any other csquad build.
// Each team is written by exactly one build: the one recorded in its pin.
var ErrWrongBuild = errors.New("this csquad build is not the one the team is pinned to")

// forwardedEnv marks a command forwarded to a team's pinned build, so the
// pinned build never forwards again.
const forwardedEnv = "CSQUAD_FORWARDED"

// selfSHA256 is replaceable in tests, whose binary is a Go test executable.
var selfSHA256 = pin.SelfSHA256

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
	return fmt.Errorf("team %s is pinned to csquad %s (%s…), but its copy is unusable: %v; run: csquad repin %s", s.ID, p.Version, p.SHA256[:12], err, s.ID)
}

// forward runs a team command with the team's pinned build instead of this
// one. It returns without effect when this process already is that build.
func forward(s *State) error {
	p, pinned := teamPin(s)
	if !pinned {
		if s.Active && stderrIsTerminal() {
			fmt.Fprintf(os.Stderr, "csquad: team %s is not pinned to a csquad version; stop and resume it to pin\n", s.ID)
		}
		return nil
	}
	sha, err := selfSHA256()
	if err != nil {
		return fmt.Errorf("hash the running csquad: %w", err)
	}
	if sha == p.SHA256 {
		_ = os.Unsetenv(forwardedEnv)
		return nil
	}
	if os.Getenv(forwardedEnv) != "" {
		return fmt.Errorf("forwarding loop: team %s is pinned to %s…, but its pinned build %s is %s…", s.ID, p.SHA256[:12], p.Path, sha[:12])
	}
	if err = ensurePin(s); err != nil {
		return err
	}
	if p.Version != buildinfo.Version && stderrIsTerminal() {
		fmt.Fprintf(os.Stderr, "csquad: team %s runs csquad %s (pinned); resume it to move to %s\n", s.ID, p.Version, buildinfo.Version)
	}
	argv := append([]string{p.Path}, os.Args[1:]...)
	return syscall.Exec(p.Path, argv, append(os.Environ(), forwardedEnv+"="+p.SHA256))
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

// transitionPin moves a team to the running build in the one transaction the
// self check lets through with a different hash. Callers hold team-lifecycle
// and have checked the downgrade rule; every write after it is by the pin.
func (st *Store) transitionPin() (pin.Pin, error) {
	p, err := pin.Create()
	if err != nil {
		return pin.Pin{}, err
	}
	st.transition = true
	defer func() { st.transition = false }()
	return p, st.update(func(s *State) error {
		if s.Executable != p.Path {
			s.event("master", "pinned", p.Version+" "+p.SHA256[:12])
		}
		s.Executable = p.Path
		return nil
	})
}

// repinTeam is the one recovery for a team whose pinned copy is missing or
// damaged. It refuses an intact pin, restores an identical build from itself,
// applies the downgrade rule, and otherwise moves the team to this build. A
// running team is stopped after the move, so the stop is written by the new
// pin, not by a second build.
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
	p, pinned := teamPin(s)
	if !pinned {
		return fmt.Errorf("team %s is not pinned to a csquad build; stop it and resume it with this csquad to pin it", s.ID)
	}
	if pin.Verify(p) == nil {
		return fmt.Errorf("team %s's pinned csquad %s is intact; nothing to repair. To move it to this build, stop and resume it", s.ID, p.Version)
	}
	if ensurePin(s) == nil {
		fmt.Fprintf(os.Stderr, "csquad: restored team %s's copy of csquad %s from this identical build\n", s.ID, p.Version)
		return nil
	}
	if err = decideRepin(s); err != nil {
		return err
	}
	if s.Active && !yes && !confirm(fmt.Sprintf("team %s is running; repin moves it to csquad %s and stops it. Continue? [y/N] ", s.ID, buildinfo.Version)) {
		return fmt.Errorf("team %s was not changed", s.ID)
	}
	next, err := st.transitionPin()
	if err != nil {
		return fmt.Errorf("pin this csquad build for the team: %w", err)
	}
	if s.Active {
		if err = cleanupTeam(st, "stopped"); err != nil {
			return err
		}
	}
	fmt.Fprintf(os.Stderr, "csquad: team %s now uses csquad %s (%s…); start it with: csquad resume %s\n", s.ID, next.Version, next.SHA256[:12], s.ID)
	return nil
}
