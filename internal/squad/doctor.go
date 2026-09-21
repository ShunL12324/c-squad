package squad

import (
	"os"
	"os/exec"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/process"
)

func doctor(o options) error {
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root := cwd
	if r, err := git(cwd, "rev-parse", "--show-toplevel"); err == nil {
		root = r
	}
	cfg, err := config.Load(root)
	if err != nil {
		return err
	}
	result := map[string]any{"config": config.Path(), "runtime": "team-scoped tmux process; no installed service"}
	for _, name := range []string{"tmux", "git", "ps", "claude", "codex"} {
		info := map[string]any{}
		command := config.Command{Executable: name}
		var env map[string]string
		if name == "claude" || name == "codex" {
			command = cfg.Command(config.Engine(name))
			env = agentenv.Merge(cfg.Env, cfg.StartupEnv)
			info["executable"] = command.Executable
			info["args"] = command.Args
		}
		run := func(args ...string) (string, error) {
			executable, argv, prepared := command.Invocation(env, "", args...)
			return process.RunEnv(cwd, prepared, executable, argv...)
		}
		executable, _, _ := command.Invocation(nil, "")
		path, e := exec.LookPath(executable)
		if e == nil && command.Shell != "" {
			info["shell"] = command.Shell
			e = preflight.EngineCommand(command, env)
		}
		if e != nil {
			info["available"] = false
			info["error"] = e.Error()
			result[name] = info
			continue
		}
		info["available"] = true
		info["path"] = path
		arg := "--version"
		if name == "ps" {
			version, err := run("-p", "1", "-o", "pid=")
			info["probe_ok"] = err == nil && version != ""
			if err != nil {
				info["error"] = err.Error()
			}
			result[name] = info
			continue
		}
		if name == "tmux" {
			arg = "-V"
		}
		version, e := run(arg)
		if e != nil {
			info["error"] = e.Error()
		} else {
			info["version"] = version
		}
		if name == "claude" {
			help, e := run("--help")
			info["session_prompt_flags"] = e == nil && strings.Contains(help, "--append-system-prompt") && strings.Contains(help, "--settings")
			info["peer_protocol_note"] = "external socket adapter validated on 2.1.276; engine upgrades require integration testing"
		}
		if name == "codex" {
			help, e := run("queue", "--help")
			info["queue_by_thread"] = e == nil && strings.Contains(help, "--thread")
			help, e = run("--help")
			info["hook_trust_flag"] = e == nil && strings.Contains(help, "--dangerously-bypass-hook-trust")
		}
		result[name] = info
	}
	if err := jsonOut(result); err != nil {
		return err
	}
	if o["strict"] == "true" {
		return preflight.CheckConfig(cfg, config.Engine(o["engine"]))
	}
	return nil
}
