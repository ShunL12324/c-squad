package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

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
			command, env = cfg.LaunchFor(config.Engine(name))
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
	result["csquad"] = csquadInstalls()
	if err := jsonOut(result); err != nil {
		return err
	}
	if o["strict"] == "true" {
		engine := config.Engine(o["engine"])
		if engine == "" {
			return preflight.Check(cfg)
		}
		command, env := cfg.LaunchFor(engine)
		return preflight.CheckCommand(engine, command, env)
	}
	return nil
}

// csquadInstalls lists every csquad executable on PATH with its version. More
// than one is a risk: a team is pinned against newer builds, but an older
// csquad earlier on PATH, or one from before pinning, still writes it directly.
func csquadInstalls() map[string]any {
	var found []map[string]string
	seen := map[string]bool{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		path := filepath.Join(dir, "csquad")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
			continue
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil || seen[real] {
			continue
		}
		seen[real] = true
		version, err := exec.Command(path, "version").Output()
		entry := map[string]string{"path": path, "version": strings.TrimSpace(string(version))}
		if err != nil {
			entry["error"] = err.Error()
		}
		found = append(found, entry)
	}
	out := map[string]any{"on_path": found}
	if len(found) > 1 {
		out["warning"] = "more than one csquad is on PATH; upgrade or remove the others, since a csquad from before pinning writes teams directly"
	}
	return out
}
