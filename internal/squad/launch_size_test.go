package squad

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
)

// Issue #34: tmux sends a command in one message of at most 16 KB. A rendered
// Codex prompt passed as developer_instructions plus a long PATH exceeded it
// and resume failed with "command too long". The engine argv and environment
// now reach the runner through a private launch file.
func TestLongPromptAndPathStayOutOfTmuxCommand(t *testing.T) {
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
	// Each alone is close to or over the tmux limit, as in the report.
	long := []string{bin}
	for i := 0; len(strings.Join(long, ":")) < 20000; i++ {
		long = append(long, fmt.Sprintf("/mnt/c/Program Files/Vendor %04d/bin", i))
	}
	path := strings.Join(long, ":")
	instructions := "LONG-INSTRUCTIONS " + strings.Repeat("keep every word; ", 1200) + "END-OF-INSTRUCTIONS;"
	cfg := config.Defaults()
	env := map[string]string{"ENGINE_CAPTURE": capture, "PATH": path, "CUSTOM_ACCOUNT": "secret account value", "CODEX_HOME": filepath.Join(root, "codex")}
	command := config.Command{Executable: engine, Args: []string{"a", "b", "c"}}
	cfg.Profiles = map[string]config.Profile{"fake": {Engine: config.Codex, Model: "m", Env: env, Command: &command}}
	cfg.MasterProfile, cfg.DefaultProfile = "fake", "fake"
	configPath := filepath.Join(root, "config.toml")
	document, err := config.Document(cfg)
	must(t, err)
	must(t, os.WriteFile(configPath, document, 0600))
	cmdEnv := append(os.Environ(), "PATH="+bin, "CSQUAD_CONFIG="+configPath, "CSQUAD_HOME="+filepath.Join(root, "state"), "CSQUAD_STATE_DIR=", "CSQUAD_MEMBER_ID=", "CSQUAD_GENERATION=", "TMUX=", "TMUX_PANE=")
	cli := func(args ...string) {
		t.Helper()
		cmd := exec.Command(binary, args...)
		cmd.Dir, cmd.Env = root, cmdEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	cli("start", "longcmd", "--detach")
	st, err := openStore(filepath.Join(root, "state", "teams", "longcmd"))
	must(t, err)
	defer st.DB.Close()
	defer stop(st)
	cli("member", "add", "worker", "--instructions", instructions)

	var record commandCapture
	for deadline := time.Now().Add(15 * time.Second); record.Member != "worker"; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("worker engine never started")
		}
		files, _ := filepath.Glob(filepath.Join(capture, "*.json"))
		for _, file := range files {
			var r commandCapture
			data, _ := os.ReadFile(file)
			if json.Unmarshal(data, &r) == nil && r.Member == "worker" && len(r.Args) > 3 && r.Args[3] != "app-server" && r.Args[3] != "agents" {
				record = r
			}
		}
	}
	joined := strings.Join(record.Args, "\n")
	if !strings.Contains(joined, "END-OF-INSTRUCTIONS;") || !strings.Contains(joined, "keep every word; keep every word;") {
		t.Fatal("engine did not receive the full developer instructions")
	}
	if !strings.HasSuffix(record.Path, path) || record.Account != "secret account value" {
		t.Fatalf("engine environment changed: account %q, PATH suffix kept %v", record.Account, strings.HasSuffix(record.Path, path))
	}
	s, err := st.read()
	must(t, err)
	worker := s.Members["worker"]
	started, err := tm(s, "display-message", "-p", "-t", agentPane(worker), "#{pane_start_command}")
	must(t, err)
	if len(started) > 2048 || strings.Contains(started, "INSTRUCTIONS") || strings.Contains(started, "secret account value") {
		t.Fatalf("tmux carried the engine argv or environment (%d bytes): %.200q", len(started), started)
	}
	info, err := os.Stat(launchSpecPath(st, "worker", worker.Generation))
	must(t, err)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("launch file mode = %v, want 0600", info.Mode().Perm())
	}
}
