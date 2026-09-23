package squad

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

// Issue #34: an edited profile never reached an existing member. The snapshot
// stays the default for restart and resume, resume says when it differs, and
// --reprofile applies the current profile while keeping the member.
func TestReprofileAppliesEditedProfileOnlyWhenAsked(t *testing.T) {
	for _, tool := range []string{"tmux", "python3"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	temp := t.TempDir()
	binary := filepath.Join(temp, "csquad")
	if out, err := exec.Command("go", "build", "-o", binary, "../../cmd/csquad").CombinedOutput(); err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	bin := filepath.Join(temp, "bin")
	must(t, os.Mkdir(bin, 0700))
	for _, tool := range []string{"tmux", "git", "ps", "sh", "sleep", "python3"} {
		path, err := exec.LookPath(tool)
		must(t, err)
		must(t, os.Symlink(path, filepath.Join(bin, tool)))
	}
	python, err := exec.LookPath("python3")
	must(t, err)
	body, err := os.ReadFile("testdata/custom_engine.py")
	must(t, err)
	root := t.TempDir()
	capture := filepath.Join(root, "capture")
	must(t, os.Mkdir(capture, 0700))
	engine := filepath.Join(root, "fake codex")
	must(t, os.WriteFile(engine, append([]byte("#!"+python+"\n"), body...), 0700))
	oldHome, newHome := filepath.Join(root, "first account"), filepath.Join(root, "second account")
	configPath := filepath.Join(root, "config.toml")
	writeConfig := func(home, model string) {
		t.Helper()
		cfg := config.Defaults()
		command := config.Command{Executable: engine, Args: []string{"a", "b", "c"}}
		cfg.Profiles = map[string]config.Profile{"codex-expert": {Engine: config.Codex, Model: model, Command: &command,
			Env: map[string]string{"ENGINE_CAPTURE": capture, "CODEX_HOME": home}}}
		cfg.MasterProfile, cfg.DefaultProfile = "codex-expert", "codex-expert"
		document, err := config.Document(cfg)
		must(t, err)
		must(t, os.WriteFile(configPath, document, 0600))
	}
	writeConfig(oldHome, "first-model")
	env := append(os.Environ(), "PATH="+bin, "CSQUAD_CONFIG="+configPath, "CSQUAD_HOME="+filepath.Join(root, "state"), "CSQUAD_STATE_DIR=", "CSQUAD_MEMBER_ID=", "CSQUAD_GENERATION=", "TMUX=", "TMUX_PANE=")
	cli := func(args ...string) string {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = root, env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
		return string(out)
	}
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
	cli("start", "reprofile", "--detach")
	st, err := openStore(filepath.Join(root, "state", "teams", "reprofile"))
	must(t, err)
	defer st.DB.Close()
	defer stop(st)
	cli("member", "add", "worker", "--instructions", "capture the launch")
	launched("worker", 1)
	must(t, st.update(func(s *State) error {
		for id, m := range s.Members {
			m.EngineID, m.State = "session-"+id, MemberStateIdle
		}
		return nil
	}))
	// Switch accounts in the configuration, as in the report.
	writeConfig(newHome, "second-model")

	cli("member", "restart", "worker")
	r := launched("worker", 2)
	if r.CodexHome != oldHome || !slices.Contains(r.Args, "first-model") || !slices.Contains(r.Args, "session-worker") {
		t.Fatalf("plain restart left the snapshot: %+v", r)
	}

	out := cli("member", "restart", "worker", "--reprofile")
	r = launched("worker", 3)
	if r.CodexHome != newHome || !slices.Contains(r.Args, "second-model") || slices.Contains(r.Args, "session-worker") {
		t.Fatalf("reprofile did not apply the current profile with a fresh conversation: %+v", r)
	}
	if !strings.Contains(out, `model "first-model" -> "second-model"`) || !strings.Contains(out, "env changed: CODEX_HOME") || !strings.Contains(out, "starts fresh") {
		t.Fatalf("reprofile summary missing changes: %s", out)
	}
	if strings.Contains(out, oldHome) || strings.Contains(out, newHome) {
		t.Fatalf("reprofile printed an environment value: %s", out)
	}
	s, err := st.read()
	must(t, err)
	if w := s.Members["worker"]; w.Env["CODEX_HOME"] != newHome || w.Model != "second-model" || w.Instructions != "capture the launch" || w.Profile != "codex-expert" {
		t.Fatalf("reprofiled member lost identity or kept the old snapshot: %+v", w)
	}

	cli("stop")
	out = cli("resume", "reprofile", "--detach")
	if !strings.Contains(out, "Profile notice:") || !strings.Contains(out, "master") || strings.Contains(out, "master, worker") {
		t.Fatalf("resume did not name exactly the drifted member: %s", out)
	}
	s, err = st.read()
	must(t, err)
	master := s.Members["master"]
	if r = launched("master", master.Generation); r.CodexHome != oldHome || !slices.Contains(r.Args, "first-model") {
		t.Fatalf("resume changed Master's saved settings: %+v", r)
	}
	cli("recover", "--reprofile")
	if r = launched("master", master.Generation+1); r.CodexHome != newHome || !slices.Contains(r.Args, "second-model") {
		t.Fatalf("recover --reprofile did not apply the current profile to Master: %+v", r)
	}
}

func TestReprofileSwitchingEngineStartsFreshAndHidesValues(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		cfg := config.Defaults()
		cfg.Profiles = map[string]config.Profile{
			"old":   {Engine: config.Claude, Model: "a", Env: map[string]string{"TOKEN": "old-secret", "GONE": "x"}},
			"other": {Engine: config.Codex, Model: "b", Env: map[string]string{"TOKEN": "new-secret", "EXTRA": "y"}},
		}
		s.Config = &cfg
		m := s.Members["a"]
		m.Profile, m.Model, m.EngineID = "old", "a", "session-a"
		m.Env = map[string]string{"TOKEN": "old-secret", "GONE": "x"}
		return nil
	}))
	s, err := st.read()
	must(t, err)
	same, err := resolveReprofile(s, s.Members["a"], "")
	must(t, err)
	if same.fresh || same.Profile != "old" {
		t.Fatalf("unchanged profile must resume: %+v", same)
	}
	next, err := resolveReprofile(s, s.Members["a"], "other")
	must(t, err)
	// Added keys also include the inherited account selectors, as at member add.
	summary := next.summary("a")
	for _, want := range []string{`profile "old" -> "other"`, "engine claude -> codex", `model "a" -> "b"`, "EXTRA; env changed: TOKEN", "env removed: GONE", "starts fresh"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary %q lacks %q", summary, want)
		}
	}
	if strings.Contains(summary, "secret") {
		t.Fatalf("summary printed a value: %q", summary)
	}
	if _, err = resolveReprofile(s, s.Members["a"], "missing"); err == nil || !strings.Contains(err.Error(), "unknown profile") {
		t.Fatalf("unknown profile: %v", err)
	}
	must(t, st.update(func(s *State) error { next.apply(s, "master", s.Members["a"]); return nil }))
	s, err = st.read()
	must(t, err)
	if m := s.Members["a"]; m.Engine != config.Codex || m.EngineID != "" || m.Env["TOKEN"] != "new-secret" || m.Env["GONE"] != "" {
		t.Fatalf("apply: %+v", m)
	}
}
