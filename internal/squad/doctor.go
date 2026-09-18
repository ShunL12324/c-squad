package squad

import (
	"os/exec"
	"strings"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/process"
)

func doctor(o options) error {
	result := map[string]any{"config": config.Path(), "runtime": "team-scoped tmux process; no installed service"}
	for _, name := range []string{"tmux", "git", "ps", "claude", "codex"} {
		info := map[string]any{}
		path, e := exec.LookPath(name)
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
			version, err := process.Run("", name, "-p", "1", "-o", "pid=")
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
		version, e := process.Run("", name, arg)
		if e != nil {
			info["error"] = e.Error()
		} else {
			info["version"] = version
		}
		if name == "claude" {
			help, e := process.Run("", name, "--help")
			info["session_prompt_flags"] = e == nil && strings.Contains(help, "--append-system-prompt") && strings.Contains(help, "--settings")
			info["peer_protocol_note"] = "external socket adapter validated on 2.1.276; engine upgrades require integration testing"
		}
		if name == "codex" {
			help, e := process.Run("", name, "queue", "--help")
			info["queue_by_thread"] = e == nil && strings.Contains(help, "--thread")
			help, e = process.Run("", name, "--help")
			info["hook_trust_flag"] = e == nil && strings.Contains(help, "--dangerously-bypass-hook-trust")
		}
		result[name] = info
	}
	if err := jsonOut(result); err != nil {
		return err
	}
	if o["strict"] == "true" {
		return preflight.Check(config.Engine(o["engine"]))
	}
	return nil
}
