package squad

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/ShunL12324/c-squad/internal/config"
)

// workerDisallowedTools keeps the native tools that bypass master out of worker
// sessions. Plan mode is entered and left through two separate tools, so denying
// EnterPlanMode alone still left ExitPlanMode exposed and plan mode reachable;
// the engine rejects unknown names, which confirms all three are current.
const workerDisallowedTools = "AskUserQuestion,EnterPlanMode,ExitPlanMode"

func (st *Store) launch(id string, resume bool, initial string) (launchErr error) {
	s, e := st.read()
	if e != nil {
		return e
	}
	m, e := s.member(id)
	if e != nil {
		return e
	}
	defer func() {
		if launchErr != nil {
			launchErr = fmt.Errorf("launch member %s (engine %s, cwd %q): %w", id, m.Engine, m.Cwd, launchErr)
		}
	}()
	if _, e = memberDirectory("", m.Cwd); e != nil {
		return e
	}
	cfg, e := s.effectiveConfig()
	if e != nil {
		return e
	}
	// Profiles resolve at launch so an edited command reaches a restart, unlike
	// engine, model and environment, which are snapshotted when the member is added.
	command, warning := cfg.ProfileCommand(m.Profile, m.Engine)
	if warning != "" {
		fmt.Fprintln(os.Stderr, "Profile warning:", warning)
		if e = st.update(func(s *State) error { s.event(id, "config_warning", warning); return nil }); e != nil {
			return e
		}
	}
	name := command.Executable
	if command.Shell != "" {
		name = command.Shell
	}
	executable, e := exec.LookPath(name)
	if e != nil {
		return e
	}
	if command.Shell != "" {
		executable = command.Executable
	}
	dir := filepath.Join(st.Dir, "runtime", id)
	if e = os.MkdirAll(dir, 0700); e != nil {
		return e
	}
	memberEnv, e := st.memberEnvironment(s, m)
	if e != nil {
		return e
	}
	renderedPrompt, e := prompt(s, m)
	if e != nil {
		return e
	}
	pf := filepath.Join(dir, "prompt.txt")
	if e = os.WriteFile(pf, []byte(renderedPrompt), 0600); e != nil {
		return e
	}
	hookcmd := shellQuote(s.Executable) + " --team " + shellQuote(st.Dir) + " --member " + shellQuote(id) + " --generation " + strconv.Itoa(m.Generation) + " hook"
	var args []string
	switch m.Engine {
	case config.Claude:
		hooks := map[string]any{}
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "StopFailure", "SessionEnd"} {
			hooks[event] = []any{commandHook(hookcmd, 10)}
		}
		settings := map[string]any{"crossSessionInbound": "accept", "hooks": hooks}
		if cfg.Bypass {
			settings["skipDangerousModePermissionPrompt"] = true
		}
		b, _ := json.Marshal(settings)
		sp := filepath.Join(dir, "claude.json")
		if e = os.WriteFile(sp, b, 0600); e != nil {
			return e
		}
		args = []string{"--name", s.ID + "-" + id, "--append-system-prompt-file", pf, "--settings", sp, "--system-prompt-snapshot", "off"}
		if cfg.Bypass {
			args = append(args, "--dangerously-skip-permissions")
		}
		if id != "master" {
			// One argument: the flag is variadic, so a separate value would also
			// swallow every later bare argument, including the initial prompt.
			args = append(args, "--disallowedTools="+workerDisallowedTools)
		}
		if resume && m.EngineID != "" {
			args = append(args, "--resume", m.EngineID)
		}
	case config.Codex:
		// TOML basic strings share the JSON encoding used here for our generated text.
		existing, err := codexConfigWithCommand(m.Cwd, cfg.Bypass, m.Env, command)
		if err != nil {
			return err
		}
		for _, warning := range existing.Warnings {
			fmt.Fprintln(os.Stderr, "Codex configuration warning:", warning)
			if e = st.update(func(s *State) error { s.event(id, "config_warning", warning); return nil }); e != nil {
				return e
			}
		}
		b, _ := json.Marshal(existing.Instructions + "\n\n" + renderedPrompt)
		args = []string{"-c", "developer_instructions=" + string(b), "--no-alt-screen", "-c", "check_for_update_on_startup=false"}
		if cfg.Bypass {
			args = append(args, "-c", codexTrustOverride(m.Cwd))
		}
		if cfg.Bypass {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox", "--dangerously-bypass-hook-trust")
		}
		if id != "master" {
			args = append(args, "-c", "features.default_mode_request_user_input=false")
		}
		for _, event := range []string{"SessionStart", "UserPromptSubmit", "PreToolUse", "PostToolUse", "Stop", "Interrupt", "SessionEnd"} {
			timeout := 10
			if event == "SessionEnd" || event == "Interrupt" {
				timeout = 3
			}
			groups := []any{}
			if raw := existing.Hooks[event]; len(raw) > 0 {
				if err = json.Unmarshal(raw, &groups); err != nil {
					return fmt.Errorf("invalid native %s hooks: %w", event, err)
				}
			}
			groups = append(groups, commandHook(hookcmd, timeout))
			value, err := tomlValue(groups)
			if err != nil {
				return err
			}
			args = append(args, "-c", "hooks."+event+"="+value)
		}
		if resume && m.EngineID != "" {
			args = append(args, "resume", m.EngineID)
		}
		args = append(args, "--cd", m.Cwd)
	default:
		return fmt.Errorf("unsupported engine %q", m.Engine)
	}
	if m.Model != "" {
		args = append(args, "--model", m.Model)
	}
	if initial != "" {
		args = append(args, initial)
	}
	launch := []string{s.Executable, "--team", st.Dir, "--member", id, "--generation", strconv.Itoa(m.Generation), "run-engine", "--"}
	launch = append(launch, executable)
	launch = append(launch, command.Arguments(args...)...)
	// tmux accepts argv when more than one shell-command argument is supplied.
	width, height := teamWindowSize(s)
	ta := []string{"new-session", "-d", "-s", m.Session, "-c", m.Cwd, "-x", width, "-y", height, "-P", "-F", "#{pane_id}"}
	for k, v := range map[string]string{"CSQUAD_STATE_DIR": st.Dir, "CSQUAD_MEMBER_ID": id, "CSQUAD_GENERATION": strconv.Itoa(m.Generation)} {
		ta = append(ta, "-e", k+"="+v)
	}
	for k, v := range memberEnv {
		ta = append(ta, "-e", k+"="+v)
	}
	ta = append(ta, launch...)
	pane, e := tm(s, ta...)
	if e != nil {
		return e
	}
	if e = st.update(func(s *State) error { s.Members[id].Pane = pane; return nil }); e != nil {
		_, cleanupErr := tm(s, "kill-session", "-t", "="+m.Session)
		return errors.Join(e, cleanupErr)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = killMember(st, id)
		}
	}()
	_, e = tm(s, "set-window-option", "-t", "="+m.Session+":", "remain-on-exit", "on")
	if e != nil {
		return e
	}
	_, e = tm(s, "set-option", "-t", "="+m.Session, "@csquad_team", st.Dir)
	if e != nil {
		return e
	}
	if id == "master" {
		if e = installMasterHook(st); e != nil {
			return e
		}
	}
	e = st.update(func(s *State) error { s.Members[id].Pane = pane; s.event(id, "launched", string(m.Engine)); return nil })
	if e == nil {
		e = st.configureNavigation()
	}
	cleanup = e != nil
	return e
}

func commandHook(command string, timeout int) map[string]any {
	return map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": timeout}}}
}
