package squad

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/pin"
)

// fakePin writes a store entry named for its content and returns its path.
// The content may be anything, including a script standing in for a build.
func fakePin(t *testing.T, version string, content []byte) pin.Pin {
	t.Helper()
	sum := sha256.Sum256(content)
	sha := hex.EncodeToString(sum[:])
	root := os.Getenv("CSQUAD_VERSIONS_DIR")
	dir := filepath.Join(root, version+"-"+sha)
	must(t, os.MkdirAll(dir, 0700))
	must(t, os.Chmod(root, 0700))
	path := filepath.Join(dir, "csquad")
	_ = os.Chmod(path, 0700)
	must(t, os.WriteFile(path, content, 0700))
	must(t, os.Chmod(path, 0500))
	return pin.Pin{Path: path, Version: version, SHA256: sha}
}

func pinTo(t *testing.T, st *Store, p pin.Pin) {
	t.Helper()
	st.transition = true
	defer func() { st.transition = false }()
	must(t, st.update(func(s *State) error { s.Executable = p.Path; return nil }))
}

func runAs(t *testing.T, sha string) {
	t.Helper()
	previous := selfSHA256
	selfSHA256 = func() (string, error) { return sha, nil }
	t.Cleanup(func() { selfSHA256 = previous })
}

// Every transaction compares the running build with the pin it has just read,
// so a process that wrote before a re-pin is refused after it; only the
// transition transaction may write with another hash.
func TestSelfCheckRunsInEveryTransaction(t *testing.T) {
	st := testStore(t)
	first := fakePin(t, "0.1.0", []byte("first build"))
	second := fakePin(t, "0.2.0", []byte("second build"))
	pinTo(t, st, first)
	runAs(t, first.SHA256)
	must(t, st.update(func(s *State) error { s.Members["a"].Handoff = "written by the pin"; return nil }))
	pinTo(t, st, second)
	before := ledgerJSON(t, st)
	err := st.update(func(s *State) error { s.Members["a"].Handoff = "stale writer"; return nil })
	if !errors.Is(err, ErrWrongBuild) || !strings.Contains(err.Error(), "csquad repin test") {
		t.Fatalf("stale build wrote: %v", err)
	}
	if ledgerJSON(t, st) != before {
		t.Fatal("a refused transaction changed the ledger")
	}
	// The same bytes pass wherever they run from: the path is not compared.
	runAs(t, second.SHA256)
	must(t, st.update(func(s *State) error { s.Members["a"].Handoff = "new pin"; return nil }))
}

// Only the pinned build drives a pinned team's runtime and delivery; another
// build returns without touching tmux or the outbox.
func TestOnlyThePinnedBuildDrivesRuntimeAndDelivery(t *testing.T) {
	st := testStore(t)
	p := fakePin(t, "0.1.0", []byte("pinned build"))
	pinTo(t, st, p)
	runAs(t, p.SHA256)
	var id string
	socket := filepath.Join(t.TempDir(), "no-server")
	t.Cleanup(func() { _ = exec.Command("tmux", "-S", socket, "kill-server").Run() })
	must(t, st.update(func(s *State) error {
		s.Socket = socket
		id = s.message("master", "a", "", "hello", "").ID
		return nil
	}))
	runAs(t, strings.Repeat("0", 64))
	before := ledgerJSON(t, st)
	must(t, st.startRuntime())
	if _, err := os.Stat(socket); err == nil {
		t.Fatal("another build started a runtime for the pinned team")
	}
	must(t, st.syncMessages())
	if ledgerJSON(t, st) != before {
		t.Fatal("another build changed the pinned team")
	}
	s, err := st.read()
	must(t, err)
	for _, m := range s.Messages {
		if m.ID == id && m.Attempts != 0 {
			t.Fatal("another build attempted delivery")
		}
	}
}

// resume and repin never move a team to an older build; a build that cannot
// be ordered needs an interactive yes, and a refusal names the pinned build.
func TestRepinDowngradeRule(t *testing.T) {
	st := testStore(t)
	p := fakePin(t, "0.12.0", []byte("pinned build"))
	pinTo(t, st, p)
	s, err := st.read()
	must(t, err)
	runAs(t, strings.Repeat("1", 64))
	previousVersion, previousConfirm := buildinfo.Version, confirm
	t.Cleanup(func() { buildinfo.Version, confirm = previousVersion, previousConfirm })
	asked := 0
	answer := false
	confirm = func(string) bool { asked++; return answer }
	for version, want := range map[string]string{"0.13.0": "", "0.11.9": "older", "0.12.0": "keeps", "dev": "keeps"} {
		buildinfo.Version = version
		err = decideRepin(s)
		if want == "" && err != nil || want != "" && (err == nil || !strings.Contains(err.Error(), want) || !strings.Contains(err.Error(), p.Path+" resume test")) {
			t.Fatalf("%s: %v", version, err)
		}
	}
	if asked != 2 {
		t.Fatalf("asked %d times; only the unordered builds may ask", asked)
	}
	answer = true
	buildinfo.Version = "dev"
	must(t, decideRepin(s))
	runAs(t, p.SHA256)
	buildinfo.Version = "0.1.0"
	must(t, decideRepin(s))
}

// repin repairs only a broken pin: it refuses an unpinned team and an intact
// pin, restores an identical build from itself, and otherwise moves the team
// to this build after the downgrade rule.
func TestRepinRecoversOnlyABrokenPin(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error { s.Active = false; return nil }))
	if err := repinTeam(st, false); err == nil || !strings.Contains(err.Error(), "not pinned") {
		t.Fatalf("unpinned team: %v", err)
	}
	self, err := pin.Create()
	must(t, err)
	pinTo(t, st, self)
	if err = repinTeam(st, false); err == nil || !strings.Contains(err.Error(), "intact") {
		t.Fatalf("intact pin: %v", err)
	}
	must(t, os.Chmod(self.Path, 0700))
	must(t, os.Remove(self.Path))
	must(t, repinTeam(st, false))
	must(t, pin.Verify(self))

	gone := fakePin(t, "0.0.1", []byte("an older build"))
	pinTo(t, st, gone)
	must(t, os.Chmod(gone.Path, 0700))
	must(t, os.Remove(gone.Path))
	previous := confirm
	t.Cleanup(func() { confirm = previous })
	confirm = func(string) bool { return false }
	if err = repinTeam(st, false); err == nil {
		t.Fatal("an unordered build re-pinned without confirmation")
	}
	confirm = func(string) bool { return true }
	must(t, repinTeam(st, false))
	s, err := st.read()
	must(t, err)
	if s.Executable != self.Path {
		t.Fatalf("pinned to %s, want this build %s", s.Executable, self.Path)
	}
	t.Setenv("CSQUAD_MEMBER_ID", "a")
	if err = Execute([]string{"repin"}, options{"team": st.Dir}, nil); err == nil {
		t.Fatal("repin ran inside a member session")
	}
}

var pinBuilds struct {
	once sync.Once
	dir  string
	err  error
}

// pinBuild returns a csquad binary built with the given version, shared by
// the tests in this file.
func pinBuild(t *testing.T, version string) string {
	t.Helper()
	pinBuilds.once.Do(func() { pinBuilds.dir, pinBuilds.err = os.MkdirTemp("", "csquad-pin-builds-") })
	must(t, pinBuilds.err)
	path := filepath.Join(pinBuilds.dir, version, "csquad")
	if _, err := os.Stat(path); err == nil {
		return path
	}
	out, err := exec.Command("go", "build", "-o", path, "-ldflags", "-X github.com/ShunL12324/c-squad/internal/buildinfo.Version="+version, "../../cmd/csquad").CombinedOutput()
	if err != nil {
		t.Fatalf("build %s: %v %s", version, err, out)
	}
	return path
}

// A newer csquad forwards a team command to the team's pinned build, with the
// argv, cwd and environment unchanged, however the team was selected. Exempt
// commands run where they are, a second forward is a loop, and a missing pin
// fails with the recovery command without writing.
func TestForwardingToThePinnedBuild(t *testing.T) {
	binary := pinBuild(t, "0.2.0")
	home := t.TempDir()
	capture := filepath.Join(t.TempDir(), "capture")
	script := []byte("#!/bin/sh\n{ printf '%s\\n' \"$@\"; echo \"FORWARDED=$CSQUAD_FORWARDED\"; pwd; } > " + shellQuote(capture) + "\n")
	p := fakePin(t, "0.1.0", script)
	st := namedTeamStore(t, home, "fwd")
	pinTo(t, st, p)
	must(t, os.WriteFile(filepath.Join(home, "last-team"), []byte(st.Dir), 0600))
	cwd := t.TempDir()
	run := func(env []string, args ...string) (string, error) {
		t.Helper()
		_ = os.Remove(capture)
		cmd := exec.Command(binary, args...)
		cmd.Dir = cwd
		cmd.Env = append(append(os.Environ(), "CSQUAD_HOME="+home, "TMUX="), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	for _, args := range [][]string{
		{"--team", st.Dir, "board"},
		{"--state-dir", st.Dir, "task", "list"},
		{"--team-name", "fwd", "member", "list"},
		{"stop", "fwd"},
		{"attach", "fwd"},
		{"board"},
	} {
		if out, err := run(nil, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		data, err := os.ReadFile(capture)
		if err != nil {
			t.Fatalf("%v was not forwarded", args)
		}
		want := strings.Join(args, "\n") + "\nFORWARDED=" + p.SHA256 + "\n" + cwd + "\n"
		if string(data) != want {
			t.Fatalf("%v forwarded as:\n%s", args, data)
		}
	}
	for _, args := range [][]string{{"version"}, {"list"}} {
		if out, err := run(nil, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		if _, err := os.Stat(capture); err == nil {
			t.Fatalf("exempt %v was forwarded", args)
		}
	}
	if out, err := run([]string{forwardedEnv + "=" + p.SHA256}, "--team", st.Dir, "board"); err == nil || !strings.Contains(out, "forwarding loop") {
		t.Fatalf("second forward: %v %s", err, out)
	}
	must(t, os.Chmod(p.Path, 0700))
	must(t, os.WriteFile(p.Path, []byte("#!/bin/sh\necho tampered\n"), 0700))
	before := ledgerJSON(t, st)
	if out, err := run(nil, "--team", st.Dir, "board"); err == nil || !strings.Contains(out, "csquad repin fwd") {
		t.Fatalf("damaged pin: %v %s", err, out)
	}
	must(t, os.Remove(p.Path))
	if out, err := run(nil, "stop", "fwd"); err == nil || !strings.Contains(out, "csquad repin fwd") {
		t.Fatalf("missing pin: %v %s", err, out)
	}
	if ledgerJSON(t, st) != before {
		t.Fatal("a failed forward wrote the ledger")
	}
}

// The whole contract on a real team of fake Codex members: it runs a private
// copy of the build that started it, keeps running it after the installed file
// is replaced and deleted, a resume with a newer build moves every generated
// entry point to the new copy, and an older build cannot resume it.
func TestPinnedTeamSurvivesReplacementAndResumeRepins(t *testing.T) {
	for _, tool := range []string{"tmux", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	older, newer := pinBuild(t, "0.1.0"), pinBuild(t, "0.2.0")
	temp := t.TempDir()
	tools := filepath.Join(temp, "tools")
	must(t, os.Mkdir(tools, 0700))
	for _, tool := range []string{"tmux", "git", "ps", "sh", "sleep", "python3"} {
		path, err := exec.LookPath(tool)
		must(t, err)
		must(t, os.Symlink(path, filepath.Join(tools, tool)))
	}
	install := filepath.Join(temp, "install", "csquad")
	must(t, os.MkdirAll(filepath.Dir(install), 0700))
	put := func(build string) {
		t.Helper()
		data, err := os.ReadFile(build)
		must(t, err)
		next := install + ".new"
		must(t, os.WriteFile(next, data, 0700))
		must(t, os.Rename(next, install))
	}
	put(older)
	python, err := exec.LookPath("python3")
	must(t, err)
	body, err := os.ReadFile("testdata/custom_engine.py")
	must(t, err)
	root := t.TempDir()
	capture := filepath.Join(root, "capture")
	must(t, os.Mkdir(capture, 0700))
	engine := filepath.Join(root, "fake codex")
	must(t, os.WriteFile(engine, append([]byte("#!"+python+"\n"), body...), 0700))
	cfg := config.Defaults()
	command := config.Command{Executable: engine, Args: []string{"a", "b", "c"}}
	cfg.Profiles = map[string]config.Profile{"fake": {Engine: config.Codex, Command: &command, Env: map[string]string{"ENGINE_CAPTURE": capture}}}
	cfg.MasterProfile, cfg.DefaultProfile = "fake", "fake"
	document, err := config.Document(cfg)
	must(t, err)
	configPath := filepath.Join(root, "config.toml")
	must(t, os.WriteFile(configPath, document, 0600))
	environ := append(os.Environ(), "PATH="+tools, "CSQUAD_CONFIG="+configPath, "CSQUAD_HOME="+filepath.Join(root, "state"), "TMUX=", "TMUX_PANE=")
	run := func(binary string, args ...string) (string, error) {
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = root, environ
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	cli := func(binary string, args ...string) {
		t.Helper()
		if out, err := run(binary, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	dir := filepath.Join(root, "state", "teams", "pinned")
	cli(install, "start", "pinned", "--detach")
	st, err := openStore(dir)
	must(t, err)
	t.Cleanup(func() { _, _ = run(newer, "stop", "pinned"); _ = st.DB.Close() })
	state := func() *State { s, err := st.read(); must(t, err); return s }
	first, ok := teamPin(state())
	if !ok || first.Version != "0.1.0" || first.Path == install {
		t.Fatalf("team not pinned to a private copy: %q", state().Executable)
	}
	cli(install, "member", "add", "worker", "--instructions", "capture the launch")
	launched := func(id string, generation int) commandCapture {
		t.Helper()
		for deadline := time.Now().Add(15 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
			files, _ := filepath.Glob(filepath.Join(capture, "*.json"))
			for _, file := range files {
				var r commandCapture
				data, _ := os.ReadFile(file)
				if json.Unmarshal(data, &r) == nil && r.Member == id && r.Generation == strconv.Itoa(generation) && len(r.Args) > 3 && r.Args[3] != "app-server" && r.Args[3] != "agents" && r.Args[3] != "queue" {
					return r
				}
			}
		}
		t.Fatalf("no launch captured for %s generation %d", id, generation)
		return commandCapture{}
	}
	launched("worker", 1)
	tmuxOut := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-S", state().Socket}, args...)...).CombinedOutput()
		must(t, err)
		return string(out)
	}
	runtimePID := func() string { return tmuxOut("display-message", "-p", "-t", "=csq-pinned-runtime:", "#{pane_pid}") }
	pid := runtimePID()
	// Replace the installed file with a newer build, then delete it. Team
	// commands through the newer build are forwarded to the pin, and exempt
	// commands never touch the pinned runtime.
	put(newer)
	cli(install, "list")
	cli(install, "member", "restart", "worker")
	restarted := launched("worker", 2)
	must(t, os.Remove(install))
	cli(newer, "sync")
	if runtimePID() != pid {
		t.Fatal("another build restarted the pinned runtime")
	}
	panes := tmuxOut("list-panes", "-a", "-F", "#{pane_start_command}")
	if !strings.Contains(panes, first.Path) || strings.Contains(panes, newer) {
		t.Fatalf("sessions do not run the pinned copy:\n%s", panes)
	}
	if hooks := strings.Join(restarted.Args, " "); !strings.Contains(hooks, first.Path) {
		t.Fatalf("restarted member's hooks do not run the pin: %s", hooks)
	}
	// Resume with the newer build moves every generated entry point.
	cli(newer, "stop", "pinned")
	cli(newer, "resume", "pinned", "--detach")
	second, _ := teamPin(state())
	if second.Version != "0.2.0" || second.Path == first.Path {
		t.Fatalf("resume did not move the team to the newer build: %q", state().Executable)
	}
	resumed := launched("worker", state().Members["worker"].Generation)
	wrapper, err := os.ReadFile(filepath.Join(dir, "runtime", "worker", strconv.Itoa(state().Members["worker"].Generation), "bin", "csquad"))
	must(t, err)
	for name, text := range map[string]string{
		"keys":    tmuxOut("list-keys"),
		"panes":   tmuxOut("list-panes", "-a", "-F", "#{pane_start_command}"),
		"hooks":   strings.Join(resumed.Args, " "),
		"wrapper": string(wrapper),
	} {
		if strings.Contains(text, first.Path) {
			t.Fatalf("%s still reference the old pin %s:\n%s", name, first.Path, text)
		}
	}
	if !strings.Contains(string(wrapper), second.Path) || !strings.Contains(tmuxOut("list-panes", "-a", "-F", "#{pane_start_command}"), second.Path) {
		t.Fatal("the new pin is not what the team runs")
	}
	// The older build cannot take the team back.
	cli(newer, "stop", "pinned")
	if out, err := run(older, "resume", "pinned", "--detach"); err == nil || !strings.Contains(out, "older") {
		t.Fatalf("older build resumed the team: %v %s", err, out)
	}
	if got, _ := teamPin(state()); got != second {
		t.Fatal("a refused downgrade changed the pin")
	}
}

// doctor names every csquad on PATH and warns when there is more than one.
func TestDoctorListsEveryCsquadOnPath(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	for dir, version := range map[string]string{first: "0.12.0", second: "0.10.0"} {
		must(t, os.WriteFile(filepath.Join(dir, "csquad"), []byte("#!/bin/sh\necho 'csquad "+version+"'\n"), 0700))
	}
	t.Setenv("PATH", first+string(os.PathListSeparator)+second)
	got := csquadInstalls()
	found := got["on_path"].([]map[string]string)
	if len(found) != 2 || found[0]["version"] != "csquad 0.12.0" || found[1]["version"] != "csquad 0.10.0" || got["warning"] == nil {
		t.Fatalf("installs: %+v", got)
	}
	t.Setenv("PATH", first)
	if got = csquadInstalls(); got["warning"] != nil {
		t.Fatalf("one install warned: %+v", got)
	}
}
