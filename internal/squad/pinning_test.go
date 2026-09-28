package squad

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

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

func TestInstalledExecutableKeepsPathSymlink(t *testing.T) {
	self, err := os.Executable()
	must(t, err)
	dir := t.TempDir()
	link := filepath.Join(dir, "csquad")
	must(t, os.Symlink(self, link))
	t.Setenv("PATH", dir)
	got, err := installedExecutable()
	must(t, err)
	if got != link {
		t.Fatalf("installed executable = %q, want stable link %q", got, link)
	}
}

func TestTemporaryExecutablePaths(t *testing.T) {
	for _, path := range []string{
		"/tmp/go-build123/b001/exe/csquad",
		"/home/user/.npm/_npx/123/node_modules/csquad/native/linux-x64/csquad",
	} {
		if !temporaryExecutable(path) {
			t.Fatalf("accepted temporary executable %s", path)
		}
	}
	if temporaryExecutable("/usr/local/bin/csquad") {
		t.Fatal("rejected persistent executable")
	}
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
	if !errors.Is(err, ErrWrongBuild) || !strings.Contains(err.Error(), "csquad stop test") {
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

// repin moves a legacy team to the installed path without creating a copy.
func TestRepinMigratesLegacyCopy(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error { s.Active = false; return nil }))
	if err := repinTeam(st, false); err == nil || !strings.Contains(err.Error(), "already uses the installed") {
		t.Fatalf("installed team: %v", err)
	}
	legacy := fakePin(t, "0.0.1", []byte("old build"))
	pinTo(t, st, legacy)
	must(t, os.Chmod(legacy.Path, 0700))
	must(t, os.Remove(legacy.Path))
	previous := confirm
	t.Cleanup(func() { confirm = previous })
	confirm = func(string) bool { return true }
	must(t, repinTeam(st, false))
	s, err := st.read()
	must(t, err)
	want, err := installedExecutable()
	must(t, err)
	if s.Executable != want {
		t.Fatalf("executable = %q, want installed %q", s.Executable, want)
	}
	if _, err := os.Stat(legacy.Path); !os.IsNotExist(err) {
		t.Fatalf("private copy was recreated: %v", err)
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

// Installed csquad commands never execute a legacy team's private copy.
func TestLegacyCommandsDoNotLaunchPrivateCopy(t *testing.T) {
	st := testStore(t)
	marker := filepath.Join(t.TempDir(), "executed")
	legacy := fakePin(t, "0.0.1", []byte("#!/bin/sh\ntouch "+shellQuote(marker)+"\n"))
	pinTo(t, st, legacy)
	runAs(t, strings.Repeat("0", 64))
	s, err := st.read()
	must(t, err)
	if err := forward(s); err == nil || !strings.Contains(err.Error(), "csquad stop") {
		t.Fatalf("legacy command: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("legacy executable ran: %v", err)
	}
}

// A team uses the installed executable for both start and resume.
func TestTeamUsesInstalledExecutableAcrossStartAndResume(t *testing.T) {
	for _, tool := range []string{"tmux", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	older := pinBuild(t, "0.1.0")
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
	versions := filepath.Join(root, "versions")
	environ := append(os.Environ(), "PATH="+tools, "CSQUAD_CONFIG="+configPath, "CSQUAD_HOME="+filepath.Join(root, "state"), "CSQUAD_VERSIONS_DIR="+versions, "TMUX=", "TMUX_PANE=")
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
	t.Cleanup(func() { _, _ = run(install, "stop", "pinned"); _ = st.DB.Close() })
	state := func() *State { s, err := st.read(); must(t, err); return s }
	if got := state().Executable; got != install {
		t.Fatalf("start executable = %q, want installed path %q", got, install)
	}
	if _, pinned := teamPin(state()); pinned {
		t.Fatal("new team unexpectedly uses a private copy")
	}
	data, err := os.ReadFile(older)
	must(t, err)
	legacy := fakePin(t, "0.0.1", data)
	pinTo(t, st, legacy)
	if out, err := run(install, "--team", dir, "board"); err == nil || !strings.Contains(out, "csquad stop") {
		t.Fatalf("legacy command should request migration without running the copy: %v %s", err, out)
	}
	cli(install, "stop", "pinned")
	if got := state().Executable; got != install {
		t.Fatalf("stop executable = %q, want installed path %q", got, install)
	}
	cli(install, "resume", "pinned", "--detach")
	if got := state().Executable; got != install {
		t.Fatalf("legacy resume executable = %q, want installed path %q", got, install)
	}
	if _, pinned := teamPin(state()); pinned {
		t.Fatal("resumed team still uses its legacy private copy")
	}
	newer := pinBuild(t, "0.2.0")
	put(newer)
	if out, err := run(install, "--team", dir, "board"); err == nil || !strings.Contains(out, "csquad stop") {
		t.Fatalf("replaced build entered active team: %v %s", err, out)
	}
	cli(install, "stop", "pinned")
	cli(install, "resume", "pinned", "--detach")
	if got := state().BuildSHA256; got == "" {
		t.Fatal("resumed team has no build fingerprint")
	}
	if entries, err := os.ReadDir(versions); err == nil && len(entries) != 0 {
		t.Fatalf("start or resume created private copies: %v", entries)
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
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
