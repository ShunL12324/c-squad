// Package update upgrades csquad through whichever package manager installed
// it. It never replaces files a package manager owns itself, and it never
// touches a team: running teams keep their pinned build until they resume.
// See docs/design/update.md.
package update

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"syscall"

	"github.com/charmbracelet/x/term"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
)

// Channel is how csquad was installed and what upgrading it takes.
type Channel struct {
	// Name identifies the channel: npm, homebrew, apt, deb, or a hint-only one.
	Name string
	// Commands run in order, each an argv without a shell.
	Commands [][]string
	// Root reports that the commands run under sudo.
	Root bool
	// Hint replaces Commands when csquad will not run the upgrade itself.
	Hint string
	// Entry is the stable path the channel installs csquad at, read after the
	// upgrade to report the new version. The old path may be gone by then.
	Entry string
	// deb is set for a local .deb install, which downloads before it installs.
	deb bool
}

// Team is what update reports about a saved team.
type Team struct {
	Name    string
	Active  bool
	Pinned  bool
	Version string
}

// Options configure one run.
type Options struct {
	Check bool
	Yes   bool
	// Teams lists the saved teams csquad can find, for the before and after
	// reports. It must only read.
	Teams func() []Team
	Out   io.Writer
}

// Hooks tests replace. Production uses the real process environment.
var (
	executablePath = resolveSelf
	isTTY          = func() bool { return term.IsTerminal(os.Stdin.Fd()) && term.IsTerminal(os.Stderr.Fd()) }
	confirm        = askYesNo
	runCommand     = func(argv []string) error {
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		return cmd.Run()
	}
	output = func(argv ...string) (string, error) {
		out, err := exec.Command(argv[0], argv[1:]...).Output()
		return string(out), err
	}
)

func resolveSelf() (string, error) {
	if runtime.GOOS == "linux" {
		if path, err := os.Readlink("/proc/self/exe"); err == nil {
			return strings.TrimSuffix(path, " (deleted)"), nil
		}
	}
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(path)
}

// ErrMemberSession refuses an update started by an agent.
var ErrMemberSession = errors.New("csquad update changes system software; run it from a terminal outside the team")

// Run checks for or performs an update.
func Run(o Options) error {
	for _, key := range []string{"CSQUAD_MEMBER_ID", "CSQUAD_STATE_DIR"} {
		if os.Getenv(key) != "" {
			return fmt.Errorf("%w (%s is set)", ErrMemberSession, key)
		}
	}
	if o.Out == nil {
		o.Out = os.Stdout
	}
	self, err := executablePath()
	if err != nil {
		return err
	}
	ch, err := Detect(self)
	if err != nil {
		return err
	}
	fmt.Fprintf(o.Out, "Installed: %s\nChannel: %s (%s)\n", buildinfo.String(), ch.Name, self)
	if o.Check {
		latest, err := Latest()
		if err != nil {
			fmt.Fprintf(o.Out, "Latest: unable to query the latest release: %v\n", err)
		} else {
			fmt.Fprintf(o.Out, "Latest: %s\n", latest.Tag)
		}
		describe(o.Out, ch)
		reportStore(o.Out)
		return nil
	}
	if ch.Hint != "" {
		fmt.Fprintln(o.Out, ch.Hint)
		return fmt.Errorf("csquad cannot update a %s installation itself", ch.Name)
	}
	var download *debPackage
	if ch.deb {
		if download, err = prepareDeb(o.Out); err != nil {
			return err
		}
		defer download.remove()
		ch.Commands = [][]string{{"sudo", "apt-get", "install", download.path}}
	}
	describe(o.Out, ch)
	warnUnpinned(o)
	switch {
	case ch.Root && !isTTY():
		return errors.New("these commands need sudo, which csquad runs only in an interactive terminal; run them yourself")
	case !o.Yes && !isTTY():
		return errors.New("not an interactive terminal; run the commands yourself or pass --yes")
	case !o.Yes && !confirm("Proceed? [y/N] "):
		return errors.New("update cancelled")
	}
	for _, argv := range ch.Commands {
		if err = runCommand(argv); err != nil {
			return fmt.Errorf("%s failed: %w", strings.Join(argv, " "), err)
		}
	}
	return report(o, ch)
}

func describe(w io.Writer, ch Channel) {
	if ch.Hint != "" {
		fmt.Fprintln(w, ch.Hint)
		return
	}
	fmt.Fprintln(w, "Update commands:")
	for _, argv := range ch.Commands {
		fmt.Fprintln(w, "  "+strings.Join(quoteAll(argv), " "))
	}
	if ch.Name == "apt" {
		fmt.Fprintln(w, "apt-get update refreshes every package source configured on this system.")
	}
}

func quoteAll(argv []string) []string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a == "" || strings.ContainsAny(a, " \t'\"$`\\") {
			a = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
		out[i] = a
	}
	return out
}

func warnUnpinned(o Options) {
	if o.Teams == nil {
		return
	}
	for _, t := range o.Teams() {
		if t.Active && !t.Pinned {
			fmt.Fprintf(o.Out, "Warning: team %s is running and not pinned to a csquad build; the upgrade reaches it at once. Stop it first, then resume it after the update to pin it.\n", t.Name)
		}
	}
}

// report reads the version from the channel's stable entry, since the upgrade
// may have removed the path this process started from, then lists teams.
func report(o Options, ch Channel) error {
	out, err := output(ch.Entry, "version")
	if err != nil {
		return fmt.Errorf("the update ran, but %s did not run: %w", ch.Entry, err)
	}
	installed := strings.TrimSpace(out)
	if installed == buildinfo.String() {
		fmt.Fprintf(o.Out, "csquad is up to date: %s\n", installed)
	} else {
		fmt.Fprintf(o.Out, "Updated: %s\n", installed)
	}
	if o.Teams != nil {
		for _, t := range o.Teams() {
			if t.Pinned {
				fmt.Fprintf(o.Out, "team %s: csquad %s (pinned); resume it to move to the new build\n", t.Name, t.Version)
			} else {
				fmt.Fprintf(o.Out, "team %s: not pinned; stop and resume it to pin\n", t.Name)
			}
		}
	}
	return nil
}

func askYesNo(question string) bool {
	fmt.Fprint(os.Stderr, question)
	answer, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes"
}

var npmLayout = regexp.MustCompile(`^(.+)/lib/node_modules/csquad/native/[^/]+/csquad$`)

// Detect identifies the channel from the resolved path of the running binary,
// cross-checked with the package manager's own records.
func Detect(self string) (Channel, error) {
	if m := npmLayout.FindStringSubmatch(self); m != nil && npmPackage(m[1]) {
		return npmChannel(m[1], self), nil
	}
	if strings.Contains(self, "/node_modules/csquad/") {
		return jsChannel(self), nil
	}
	if i := strings.Index(self, "/Cellar/csquad/"); i >= 0 {
		if ch, ok := brewChannel(self[:i+len("/Cellar")], self); ok {
			return ch, nil
		}
	}
	if ch, ok := dpkgChannel(self); ok {
		return ch, nil
	}
	return Channel{Name: "manual", Hint: "csquad was not installed by a package manager csquad recognises. Download the latest release from https://github.com/ShunL12324/c-squad/releases/latest; see docs/install.md."}, nil
}

func npmPackage(prefix string) bool {
	data, err := os.ReadFile(filepath.Join(prefix, "lib", "node_modules", "csquad", "package.json"))
	if err != nil {
		return false
	}
	var pkg struct{ Name string }
	return json.Unmarshal(data, &pkg) == nil && pkg.Name == "csquad"
}

// npmChannel targets the exact global prefix that owns this binary, whatever
// node version the npm on PATH belongs to: global packages are plain files.
func npmChannel(prefix, self string) Channel {
	npm := filepath.Join(prefix, "bin", "npm")
	if !executable(npm) {
		if found, err := exec.LookPath("npm"); err == nil {
			npm = found
		} else {
			npm = "npm"
		}
	}
	argv := []string{npm, "install", "--global", "--prefix", prefix, "csquad@latest"}
	ch := Channel{Name: "npm", Commands: [][]string{argv}, Entry: self}
	if !writable(filepath.Join(prefix, "lib", "node_modules")) {
		// sudo resets PATH, so npm's "#!/usr/bin/env node" may not find this
		// node; the user runs it, choosing how to reach the right node.
		ch.Hint = "The npm prefix " + prefix + " is not writable by this user. Run: sudo " + strings.Join(quoteAll(argv), " ")
	}
	return ch
}

func jsChannel(self string) Channel {
	hint := "csquad was installed by a JavaScript package manager csquad does not update itself. Reinstall csquad@latest with that package manager."
	switch {
	case strings.Contains(self, "/pnpm/"):
		hint = "csquad was installed with pnpm. Run: pnpm add --global csquad@latest"
	case strings.Contains(self, "/.bun/"):
		hint = "csquad was installed with bun. Run: bun add --global csquad@latest"
	case strings.Contains(self, "/yarn/"):
		hint = "csquad was installed with yarn. Run: yarn global add csquad@latest"
	}
	return Channel{Name: "javascript", Hint: hint}
}

// brewChannel accepts any Homebrew prefix: the brew beside the Cellar must
// report that Cellar, and the formula's tap comes from its install receipt.
func brewChannel(cellar, self string) (Channel, bool) {
	brew := filepath.Join(filepath.Dir(cellar), "bin", "brew")
	if !executable(brew) {
		return Channel{}, false
	}
	out, err := output(brew, "--cellar")
	if err != nil || !samePath(strings.TrimSpace(out), cellar) {
		return Channel{}, false
	}
	formula := brewFormula(brew, cellar, self)
	ch := Channel{Name: "homebrew", Commands: [][]string{{brew, "upgrade", formula}}, Entry: filepath.Join(filepath.Dir(cellar), "opt", "csquad", "bin", "csquad")}
	if !writable(filepath.Join(cellar, "csquad")) {
		ch.Hint = "Homebrew's " + filepath.Join(cellar, "csquad") + " is not writable by this user, and Homebrew refuses to run as root. Run as its owner: " + strings.Join(quoteAll(ch.Commands[0]), " ")
	}
	return ch, true
}

func brewFormula(brew, cellar, self string) string {
	rest := strings.TrimPrefix(self, cellar+"/csquad/")
	keg := filepath.Join(cellar, "csquad", strings.SplitN(rest, "/", 2)[0])
	var receipt struct {
		Source struct {
			Tap string `json:"tap"`
		} `json:"source"`
	}
	if data, err := os.ReadFile(filepath.Join(keg, "INSTALL_RECEIPT.json")); err == nil && json.Unmarshal(data, &receipt) == nil && receipt.Source.Tap != "" {
		if strings.EqualFold(receipt.Source.Tap, "homebrew/core") {
			return "csquad"
		}
		return strings.ToLower(receipt.Source.Tap) + "/csquad"
	}
	if out, err := output(brew, "list", "--full-name", "--formula"); err == nil {
		for _, line := range strings.Fields(out) {
			if line == "csquad" || strings.HasSuffix(line, "/csquad") {
				return line
			}
		}
	}
	return "csquad"
}

// dpkgChannel identifies a Debian package install, from the csquad APT source
// or from a local .deb.
func dpkgChannel(self string) (Channel, bool) {
	out, err := output("dpkg-query", "-S", self)
	if err != nil || !strings.HasPrefix(strings.TrimSpace(out), "csquad:") {
		return Channel{}, false
	}
	policy, _ := output("apt-cache", "policy", "csquad")
	if strings.Contains(policy, "shunl12324.github.io/c-squad/apt") {
		return Channel{Name: "apt", Root: true, Entry: self, Commands: [][]string{
			{"sudo", "apt-get", "update"},
			{"sudo", "apt-get", "install", "--only-upgrade", "csquad"},
		}}, true
	}
	return Channel{Name: "deb", Root: true, Entry: self, deb: true}, true
}

func executable(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0111 != 0
}

func writable(dir string) bool {
	return syscall.Access(dir, 2) == nil
}

func samePath(a, b string) bool {
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	return errA == nil && errB == nil && ra == rb
}
