package squad

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

type commandCapture struct {
	Executable string   `json:"executable"`
	Args       []string `json:"args"`
	Cwd        string   `json:"cwd"`
	Member     string   `json:"member"`
	Generation string   `json:"generation"`
	Account    string   `json:"account"`
	CodexHome  string   `json:"codex_home"`
	ClaudeHome string   `json:"claude_home"`
	Token      string   `json:"token"`
	ModelEnv   string   `json:"model_env"`
	Path       string   `json:"path"`
}

func TestCustomCommandsAcrossLifecycle(t *testing.T) {
	for _, tool := range []string{"tmux", "python3", "bash", "zsh"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skip(tool + " unavailable")
		}
	}
	temp := t.TempDir()
	binary := filepath.Join(temp, "csquad")
	out, err := exec.Command("go", "build", "-o", binary, "../../cmd/csquad").CombinedOutput()
	if err != nil {
		t.Fatalf("build: %v %s", err, out)
	}
	// Neither native name is on PATH: every launch and probe must use the override.
	bin := filepath.Join(temp, "bin")
	must(t, os.Mkdir(bin, 0700))
	for _, tool := range []string{"tmux", "git", "ps", "sh", "bash", "zsh", "sleep"} {
		path, err := exec.LookPath(tool)
		must(t, err)
		must(t, os.Symlink(path, filepath.Join(bin, tool)))
	}
	python, err := exec.LookPath("python3")
	must(t, err)
	body, err := os.ReadFile("testdata/custom_engine.py")
	must(t, err)
	for _, mode := range []string{"", "bash", "zsh", "bash-explicit-path", "zsh-explicit-path"} {
		for _, masterEngine := range []config.Engine{config.Claude, config.Codex} {
			t.Run(string(masterEngine)+"/"+mode, func(t *testing.T) {
				shell := strings.TrimSuffix(mode, "-explicit-path")
				explicitPath := shell != mode
				root := t.TempDir()
				capture := filepath.Join(root, "capture")
				must(t, os.Mkdir(capture, 0700))
				cfg := config.Defaults()
				workerEngine := config.Claude
				if masterEngine == config.Claude {
					workerEngine = config.Codex
				}
				// Everything a member launches with comes from its profile, so the
				// two profiles carry the same shared variables and differ only where
				// the test needs them to.
				shared := map[string]string{"ENGINE_CAPTURE": capture, "CUSTOM_ACCOUNT": "team account", "CODEX_HOME": filepath.Join(root, "codex account"), "CLAUDE_CONFIG_DIR": filepath.Join(root, "claude account")}
				shared["ANTHROPIC_AUTH_TOKEN"], shared["TEST_MODEL"] = "config fake token", "config model"
				commands := map[config.Engine]config.Command{}
				prefix := []string{"profile with spaces", "", "$(touch should-not-exist)"}
				for _, engine := range []config.Engine{config.Claude, config.Codex} {
					path := filepath.Join(root, "custom client "+string(engine))
					must(t, os.WriteFile(path, append([]byte("#!"+python+"\n"), body...), 0700))
					commands[engine] = config.Command{Executable: path, Args: prefix}
				}
				if mode != "" {
					home := filepath.Join(root, "isolated home")
					must(t, os.Mkdir(home, 0700))
					shared["HOME"], shared["ZDOTDIR"] = home, home
					var rc strings.Builder
					if explicitPath {
						shared["PATH"] = root + string(os.PathListSeparator) + bin
						// A conflicting rc path must lose to the explicit override.
						rc.WriteString("export PATH=/missing-client-directory\n")
					} else {
						rc.WriteString("export PATH=" + shellQuote(root) + ":$PATH\n")
					}
					rc.WriteString("export CUSTOM_ACCOUNT=wrong CODEX_HOME=/wrong ANTHROPIC_AUTH_TOKEN=wrong TEST_MODEL=wrong CSQUAD_MEMBER_ID=wrong CSQUAD_GENERATION=0\n")
					for engine, command := range commands {
						name := "my-" + string(engine) + "-client"
						expansion := "ANTHROPIC_AUTH_TOKEN=" + shellQuote("alias fake token") + " " + shellQuote(filepath.Base(command.Executable)) + " " + shellQuote(prefix[0])
						rc.WriteString("alias " + name + "=" + shellQuote(expansion) + "\n")
						command.Executable, command.Shell, command.Args = name, shell, prefix[1:]
						commands[engine] = command
					}
					for _, name := range []string{".bashrc", ".zshrc"} {
						must(t, os.WriteFile(filepath.Join(home, name), []byte(rc.String()), 0600))
					}
				}
				configPath := filepath.Join(root, "config.toml")
				// Edited below: a member's model and environment stay as added.
				workerModel, workerAccount := "worker-model", "worker account"
				writeConfig := func() {
					t.Helper()
					masterCommand, workerCommand := commands[masterEngine], commands[workerEngine]
					cfg.Profiles = map[string]config.Profile{
						"master-account": {Engine: masterEngine, Model: "test-model", Env: agentenv.Merge(shared), Command: &masterCommand},
						"worker-account": {Engine: workerEngine, Model: workerModel, Command: &workerCommand,
							Env: agentenv.Merge(shared, map[string]string{"CUSTOM_ACCOUNT": workerAccount})},
					}
					cfg.MasterProfile, cfg.DefaultProfile = "master-account", "worker-account"
					body, err := config.Document(cfg)
					must(t, err)
					must(t, os.WriteFile(configPath, body, 0600))
				}
				writeConfig()
				env := agentenv.Environ(map[string]string{"PATH": bin, "SHELL": "/bin/sh", "CSQUAD_CONFIG": configPath, "CSQUAD_HOME": filepath.Join(root, "state"), "CSQUAD_STATE_DIR": "", "CSQUAD_MEMBER_ID": "", "CSQUAD_GENERATION": "", "TMUX": "", "TMUX_PANE": ""})
				cli := func(args ...string) []byte {
					t.Helper()
					cmd := exec.Command(binary, args...)
					cmd.Dir, cmd.Env = root, env
					out, err := cmd.CombinedOutput()
					if err != nil {
						t.Fatalf("%v: %v %s", args, err, out)
					}
					return out
				}
				cli("doctor", "--strict", "--engine", string(masterEngine))
				cli("start", "custom", "--detach")
				st, err := openStore(filepath.Join(root, "state", "teams", "custom"))
				must(t, err)
				defer st.DB.Close()
				defer func() {
					if err := stop(st); err != nil {
						t.Errorf("stop test team: %v", err)
					}
				}()
				cli("member", "add", "worker", "--instructions", "capture the launch")
				waitLaunch := func(id string, generation int, engine config.Engine, wantPrefix []string, resumed bool) {
					t.Helper()
					until := time.Now().Add(15 * time.Second)
					for time.Now().Before(until) {
						files, err := filepath.Glob(filepath.Join(capture, "*.json"))
						must(t, err)
						for _, file := range files {
							data, err := os.ReadFile(file)
							must(t, err)
							var record commandCapture
							if json.Unmarshal(data, &record) != nil || record.Member != id || record.Generation != strconv.Itoa(generation) {
								continue
							}
							if len(record.Args) < 4 || record.Args[3] == "app-server" || record.Args[3] == "agents" || record.Args[3] == "queue" {
								continue
							}
							if !reflect.DeepEqual(record.Args[:3], wantPrefix) || !strings.HasSuffix(record.Executable, string(engine)) || record.Cwd != root {
								t.Fatalf("argv/cwd changed: %+v", record)
							}
							if mode != "" && !slices.Contains(filepath.SplitList(record.Path), root) {
								t.Fatalf("shell client directory missing: %+v", record)
							}
							if explicitPath && strings.Contains(record.Path, "/missing-client-directory") {
								t.Fatalf("rc replaced explicit PATH: %+v", record)
							}
							wantAccount, wantModel := "team account", "test-model"
							if id == "worker" {
								wantAccount, wantModel = "worker account", "worker-model"
							}
							if !slices.Contains(record.Args, wantModel) {
								t.Fatalf("model snapshot changed: %+v", record)
							}
							wantToken := "config fake token"
							if mode != "" {
								wantToken = "alias fake token"
							}
							if record.Token != wantToken || record.ModelEnv != "config model" || !strings.HasPrefix(record.Path, filepath.Join(st.Dir, "runtime", id, strconv.Itoa(generation), "bin")+string(os.PathListSeparator)) {
								t.Fatalf("alias/rc override precedence changed: %+v", record)
							}
							if record.Account != wantAccount || record.CodexHome != shared["CODEX_HOME"] || record.ClaudeHome != shared["CLAUDE_CONFIG_DIR"] {
								t.Fatalf("environment changed: %+v", record)
							}
							if !slices.Contains(record.Args, "--model") || engine == config.Claude && !slices.Contains(record.Args, "--dangerously-skip-permissions") || engine == config.Codex && !slices.Contains(record.Args, "--dangerously-bypass-approvals-and-sandbox") {
								t.Fatalf("generated flags lost: %+v", record)
							}
							if engine == config.Claude && id != "master" && !slices.Contains(record.Args, "--disallowedTools="+workerDisallowedTools) {
								t.Fatalf("worker tool denial must be a single argument: %+v", record)
							}
							if resumed && !slices.Contains(record.Args, "session-"+id) {
								t.Fatalf("resume identity lost: %+v", record)
							}
							return
						}
						time.Sleep(50 * time.Millisecond)
					}
					t.Fatalf("no launch captured for %s generation %d", id, generation)
				}
				waitLaunch("master", 1, masterEngine, prefix, false)
				waitLaunch("worker", 1, workerEngine, prefix, false)
				actAsPinnedBuild(t, st)
				must(t, st.update(func(s *State) error {
					for id, m := range s.Members {
						m.EngineID, m.State = "session-"+id, MemberStateIdle
					}
					return nil
				}))
				// Exercise the same configured helpers used by transport and observation.
				s, err := st.read()
				must(t, err)
				for _, m := range s.Members {
					args := []string{"agents", "--json"}
					if m.Engine == config.Codex {
						args = []string{"queue", "--thread", m.EngineID, "--message", "literal message"}
					}
					_, err := s.engineHelper(m, args...)
					must(t, err)
				}
				newPrefix := []string{prefix[0], "", "literal ; value"}
				for engine, command := range commands {
					command.Args = newPrefix
					if mode != "" {
						command.Args = newPrefix[1:]
					}
					commands[engine] = command
				}
				workerModel, workerAccount = "edited-model", "edited account"
				writeConfig()
				// The command follows the current configuration at every launch
				// (#22); engine, model and environment remain snapshotted.
				cli("member", "restart", "worker")
				waitLaunch("worker", 2, workerEngine, newPrefix, true)
				cli("recover")
				waitLaunch("master", 2, masterEngine, newPrefix, true)
				cli("stop")
				cli("resume", "custom", "--detach")
				state, err := st.read()
				must(t, err)
				waitLaunch("master", state.Members["master"].Generation, masterEngine, newPrefix, true)
				waitLaunch("worker", state.Members["worker"].Generation, workerEngine, newPrefix, true)
				if _, err := os.Stat(filepath.Join(root, "should-not-exist")); !os.IsNotExist(err) {
					t.Fatal("prefix argument was interpreted by a shell")
				}
			})
		}
	}
}
