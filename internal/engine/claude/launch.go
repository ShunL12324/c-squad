package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// Reserved are the arguments csquad sets itself; passing them after `--`
// would break the launch.
var Reserved = []string{"--bg", "--background", "--name", "-n", "--append-system-prompt", "--print", "-p", "--resume", "-r", "--continue", "-c", "--fork-session"}

func flagName(a string) string {
	name, _, _ := strings.Cut(a, "=")
	return name
}

// CheckArgs refuses reserved flags and checks a --settings value: inline
// JSON must be an object; a file is made absolute against cwd (the launching
// directory) and must hold an object.
func (e *Engine) CheckArgs(args []string, cwd string) ([]string, error) {
	out := append([]string(nil), args...)
	for _, a := range out {
		for _, r := range Reserved {
			if flagName(a) == r {
				return nil, fmt.Errorf("%s is set by csquad launch and cannot be passed after --", a)
			}
		}
	}
	i, value, inline, err := findSettings(out)
	if err != nil || i < 0 {
		return out, err
	}
	if !looksInline(value) && !filepath.IsAbs(value) {
		value = filepath.Join(cwd, value)
		if inline {
			out[i] = "--settings=" + value
		} else {
			out[i+1] = value
		}
	}
	if _, err := readSettings(value); err != nil {
		return nil, err
	}
	return out, nil
}

// findSettings returns the index of --settings in args, its value and
// whether it was written --settings=VALUE; -1 when absent.
func findSettings(args []string) (int, string, bool, error) {
	for i, a := range args {
		if a == "--settings" {
			if i+1 >= len(args) {
				return -1, "", false, fmt.Errorf("--settings needs a value")
			}
			return i, args[i+1], false, nil
		}
		if v, ok := strings.CutPrefix(a, "--settings="); ok {
			return i, v, true, nil
		}
	}
	return -1, "", false, nil
}

func looksInline(v string) bool { return strings.HasPrefix(strings.TrimSpace(v), "{") }

// readSettings parses a --settings value: inline JSON or a file.
func readSettings(value string) (map[string]any, error) {
	data := []byte(value)
	if !looksInline(value) {
		b, err := os.ReadFile(value)
		if err != nil {
			return nil, fmt.Errorf("cannot read --settings %s: %v", value, err)
		}
		data = b
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil || m == nil {
		return nil, fmt.Errorf("--settings %s is not a JSON object", value)
	}
	return m, nil
}

func objectOf(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}

func readJSONObject(file string) map[string]any {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(b, &m) != nil {
		return nil
	}
	return m
}

// ambientSettings are the settings Claude Code reads for a session in cwd:
// the user's, and the project's (cwd and its repository root).
func (e *Engine) ambientSettings(cwd string) []map[string]any {
	dirs := []string{cwd}
	cmd := exec.Command("git", "-C", cwd, "rev-parse", "--show-toplevel")
	cmd.Env = e.cfg.Env
	if out, err := cmd.Output(); err == nil {
		if top := canonical(strings.TrimSpace(string(out))); top != "" && top != canonical(cwd) {
			dirs = append(dirs, top)
		}
	}
	files := []string{filepath.Join(e.claudeDir(), "settings.json")}
	for _, d := range dirs {
		files = append(files, filepath.Join(d, ".claude", "settings.json"), filepath.Join(d, ".claude", "settings.local.json"))
	}
	var out []map[string]any
	for _, f := range files {
		if m := readJSONObject(f); m != nil {
			out = append(out, m)
		}
	}
	return out
}

var (
	// providerEnv: the launching session's provider variables. Claude Code's
	// background service does not see the shell environment (an alias that
	// sets ANTHROPIC_BASE_URL for one session, a token exported in .zshrc).
	providerEnv = regexp.MustCompile(`^(ANTHROPIC_[A-Z0-9_]+|CLAUDE_CODE_USE_[A-Z0-9_]+|CLAUDE_CODE_OAUTH_TOKEN|CLAUDE_CODE_SUBAGENT_MODEL)$`)
	// providerChoice: keys that pick a provider or its credentials.
	providerChoice = regexp.MustCompile(`^(ANTHROPIC_BASE_URL|ANTHROPIC_AUTH_TOKEN|ANTHROPIC_API_KEY|CLAUDE_CODE_USE_[A-Z0-9_]+|CLAUDE_CODE_OAUTH_TOKEN)$`)
)

func choosesProvider(s map[string]any) bool {
	if s == nil {
		return false
	}
	if h, ok := s["apiKeyHelper"].(string); ok && h != "" {
		return true
	}
	for k := range objectOf(s, "env") {
		if providerChoice.MatchString(k) {
			return true
		}
	}
	return false
}

// taskSettings returns the task's settings: the user's --settings plus
//   - worktree.bgIsolation "none" for an in-place task (a background session
//     otherwise rejects edits to the shared checkout);
//   - worktree.baseRef "head" for a worktree task, so it sees unpushed
//     commits, unless --settings, the user's or the project's settings chose a
//     baseRef;
//   - the launching session's provider variables, all or nothing: when any of
//     those settings choose a provider themselves, none are forwarded, so one
//     provider's token never reaches another provider's base URL.
//
// It also returns the forwarded keys, sorted.
func (e *Engine) taskSettings(user map[string]any, inPlace bool, cwd string) (map[string]any, []string) {
	next := map[string]any{}
	for k, v := range user {
		next[k] = v
	}
	ambient := e.ambientSettings(cwd)
	wt := map[string]any{}
	for k, v := range objectOf(user, "worktree") {
		wt[k] = v
	}
	if inPlace {
		wt["bgIsolation"] = "none"
		next["worktree"] = wt
	} else if _, set := wt["baseRef"]; !set {
		chosen := false
		for _, a := range ambient {
			if _, ok := objectOf(a, "worktree")["baseRef"]; ok {
				chosen = true
			}
		}
		if !chosen {
			wt["baseRef"] = "head"
			next["worktree"] = wt
		}
	}
	var forwarded []string
	chosen := choosesProvider(user)
	for _, a := range ambient {
		chosen = chosen || choosesProvider(a)
	}
	if !chosen {
		env := map[string]any{}
		for k, v := range objectOf(user, "env") {
			env[k] = v
		}
		for _, kv := range e.cfg.Env {
			k, v, _ := strings.Cut(kv, "=")
			if providerEnv.MatchString(k) && v != "" {
				// The user's own --settings env wins.
				if _, own := objectOf(user, "env")[k]; !own {
					env[k] = v
					forwarded = append(forwarded, k)
				}
			}
		}
		if len(forwarded) > 0 {
			next["env"] = env
		}
	}
	sort.Strings(forwarded)
	return next, forwarded
}

// Queue prepares a task that starts later, in a detached csquad process
// whose environment carries none of the launching session's provider
// variables: the settings are merged now (forwarded variables included) into
// settings/T12.json (0600), and the returned arguments name that file.
func (e *Engine) Queue(id int, args []string, inPlace bool, cwd string) ([]string, []string, error) {
	extra := append([]string(nil), args...)
	var user map[string]any
	i, value, inline, err := findSettings(extra)
	if err != nil {
		return nil, nil, err
	}
	if i >= 0 {
		if user, err = readSettings(value); err != nil {
			return nil, nil, err
		}
		n := 2
		if inline {
			n = 1
		}
		extra = append(extra[:i], extra[i+n:]...)
	}
	settings, forwarded := e.taskSettings(user, inPlace, cwd)
	sfile := SettingsFile(e.cfg.Home, id)
	if err := writePrivate(sfile, settings); err != nil {
		return nil, nil, err
	}
	var notes []string
	if len(forwarded) > 0 {
		notes = append(notes, fmt.Sprintf("T%d will use this session's provider settings (%s), stored in %s (mode 600) while the task exists.", id, strings.Join(forwarded, ", "), sfile))
	}
	return append(extra, "--settings", sfile), notes, nil
}

// WithStopHook adds a Stop hook group running command to settings, after
// the Stop groups the user's settings already have (lists are joined).
func WithStopHook(settings map[string]any, command string) map[string]any {
	next := map[string]any{}
	for k, v := range settings {
		next[k] = v
	}
	hooks := map[string]any{}
	for k, v := range objectOf(settings, "hooks") {
		hooks[k] = v
	}
	var stop []any
	if list, ok := hooks["Stop"].([]any); ok {
		stop = append(stop, list...)
	}
	stop = append(stop, map[string]any{"hooks": []any{map[string]any{"type": "command", "command": command, "timeout": 30}}})
	hooks["Stop"] = stop
	next["hooks"] = hooks
	return next
}

// MaxInlinePrompt: one argv entry is limited to 128 KB on Linux
// (MAX_ARG_STRLEN), all of argv to ARG_MAX on macOS and the whole command
// line to 32 K characters on Windows; claude cannot read the positional
// prompt from a file or stdin. Larger prompts go to prompts/T12.md.
var MaxInlinePrompt = func() int {
	if proc.Windows {
		return 16000
	}
	return 60000
}()

// SettingsFile is the task's settings file, PromptFile its large prompt.
func SettingsFile(home string, id int) string {
	return filepath.Join(home, "settings", fmt.Sprintf("T%d.json", id))
}

// PromptFile is where a prompt too large for the command line goes.
func PromptFile(home string, id int) string {
	return filepath.Join(home, "prompts", fmt.Sprintf("T%d.md", id))
}

var (
	backgrounded = regexp.MustCompile(`backgrounded\s+·\s+([0-9a-f]{6,})`)
	// fullID is a whole session UUID.
	fullID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// FullID reports whether id is a whole session UUID (not a short id).
func FullID(id string) bool { return fullID.MatchString(id) }

// Start runs `claude --bg [--resume FORK --fork-session] --name "T12 ·
// title" [--permission-mode] [--model] ENGINE_ARGS --settings FILE
// --append-system-prompt RULES -- PROMPT` in spec.Cwd. The merged settings always go to settings/T12.json
// (mode 0600) and the command line names the path, so no credential lands on
// it.
func (e *Engine) Start(ctx context.Context, spec engine.StartSpec) (engine.Started, error) {
	var res engine.Started
	// A short id would open the interactive resume picker and hang.
	if spec.Fork != "" && !FullID(spec.Fork) {
		return res, fmt.Errorf("--fork needs the full session id, got %q", spec.Fork)
	}
	extra := append([]string(nil), spec.Args...)
	var user map[string]any
	i, value, inline, err := findSettings(extra)
	if err != nil {
		return res, err
	}
	if i >= 0 {
		if user, err = readSettings(value); err != nil {
			return res, err
		}
		n := 2
		if inline {
			n = 1
		}
		extra = append(extra[:i], extra[i+n:]...)
	}
	settings, forwarded := e.taskSettings(user, spec.InPlace, spec.Cwd)
	if spec.StopHook != "" {
		settings = WithStopHook(settings, spec.StopHook)
	}
	sfile := SettingsFile(e.cfg.Home, spec.ID)
	if err := writePrivate(sfile, settings); err != nil {
		return res, err
	}
	has := func(flags ...string) bool {
		for _, a := range extra {
			for _, f := range flags {
				if flagName(a) == f {
					return true
				}
			}
		}
		return false
	}
	argv := []string{"--bg", "--name", spec.Name()}
	if spec.Fork != "" {
		argv = []string{"--bg", "--resume", spec.Fork, "--fork-session", "--name", spec.Name()}
	}
	if e.PermissionMode != "" && !has("--permission-mode", "--dangerously-skip-permissions") {
		argv = append(argv, "--permission-mode", e.PermissionMode)
	}
	if spec.Model != "" && !has("--model") {
		argv = append(argv, "--model", spec.Model)
	}
	argv = append(argv, extra...)
	argv = append(argv, "--settings", sfile)
	prompt := spec.Prompt
	var pfile string
	if len(prompt) > MaxInlinePrompt {
		pfile = PromptFile(e.cfg.Home, spec.ID)
		if err := os.MkdirAll(filepath.Dir(pfile), 0o700); err != nil {
			return res, err
		}
		if err := os.WriteFile(pfile, []byte(prompt+"\n"), 0o600); err != nil {
			return res, err
		}
		prompt = "Your full task prompt is too large to pass on the command line. Read all of " + pfile +
			" first (in chunks with offset and limit if needed, until its end), then carry out the task it describes."
	}
	system := spec.Rules + fmt.Sprintf("\nYour task ID is T%d.\n", spec.ID)
	argv = append(argv, "--append-system-prompt", system, "--", prompt)
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	out, err := e.run(ctx, spec.Cwd, argv...)
	m := backgrounded.FindStringSubmatch(out)
	if err != nil || m == nil {
		if i >= 0 && value == sfile {
			_ = writePrivate(sfile, user) // a queued task's settings, kept for its retry
		} else {
			os.Remove(sfile)
		}
		if pfile != "" {
			os.Remove(pfile)
		}
		if out == "" && err != nil {
			out = err.Error()
		}
		return res, fmt.Errorf("launching %s failed:\n%s", spec.Name(), out)
	}
	res.SessionID = m[1]
	if len(forwarded) > 0 {
		res.Notes = append(res.Notes, fmt.Sprintf("T%d uses this session's provider settings (%s), stored in %s (mode 600) while the task exists.", spec.ID, strings.Join(forwarded, ", "), sfile))
	}
	return res, nil
}

// writePrivate writes v as JSON to file with mode 0600, its directory 0700.
func writePrivate(file string, v any) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}
