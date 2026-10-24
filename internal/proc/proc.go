// Package proc finds and runs programs the way csquad needs on every OS:
// PATH lookup with Windows' PATHEXT, npm-style .cmd shims resolved to the
// program behind them, and cmd.exe quoting as a last resort.
package proc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// Windows reports whether csquad runs on Windows.
const Windows = runtime.GOOS == "windows"

// Env is an environment in os.Environ form.
type Env []string

// Get returns the value of key; on Windows names are case-insensitive.
func (e Env) Get(key string) string {
	return e.get(key, Windows)
}

func (e Env) get(key string, win bool) string {
	for i := len(e) - 1; i >= 0; i-- {
		k, v, ok := strings.Cut(e[i], "=")
		if ok && (k == key || win && strings.EqualFold(k, key)) {
			return v
		}
	}
	return ""
}

// With returns a copy of e with key set to value (replacing every spelling of
// the key on Windows).
func (e Env) With(key, value string) Env {
	out := make(Env, 0, len(e)+1)
	for _, kv := range e {
		k, _, _ := strings.Cut(kv, "=")
		if k == key || Windows && strings.EqualFold(k, key) {
			continue
		}
		out = append(out, kv)
	}
	return append(out, key+"="+value)
}

// Without returns a copy of e without the keys for which drop is true.
func (e Env) Without(drop func(key string) bool) Env {
	out := make(Env, 0, len(e))
	for _, kv := range e {
		k, _, _ := strings.Cut(kv, "=")
		if !drop(k) {
			out = append(out, kv)
		}
	}
	return out
}

// LookPath looks for name in PATH. On Windows it tries the PATHEXT extensions
// that can be started directly (an extensionless file such as npm's sh shim
// cannot be). It returns "" when nothing is found.
func LookPath(name string, env Env, win bool) string {
	path := env.get("PATH", win)
	sep := string(os.PathListSeparator)
	if win {
		sep = ";"
	}
	exts := []string{""}
	if win {
		exts = nil
		pathext := env.get("PATHEXT", win)
		if pathext == "" {
			pathext = ".COM;.EXE;.BAT;.CMD"
		}
		for _, e := range strings.Split(pathext, ";") {
			if ok, _ := regexp.MatchString(`(?i)^\.(com|exe|bat|cmd)$`, e); ok {
				exts = append(exts, strings.ToLower(e))
			}
		}
	}
	for _, dir := range strings.Split(path, sep) {
		dir = strings.Trim(dir, `"`)
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			file := filepath.Join(dir, name+ext)
			if st, err := os.Stat(file); err == nil && st.Mode().IsRegular() && (win || st.Mode()&0o111 != 0) {
				return file
			}
		}
	}
	return ""
}

var shimLine = regexp.MustCompile(`(?i)"%~?dp0%?[\\/]*([^"%\r\n]+)"\s+%\*`)

// ShimTarget returns the program an npm-style .cmd shim runs, so that it can
// be started directly: cmd.exe cannot pass newlines in arguments and has its
// own quoting rules. npm shims end with: "%_prog%"  "%dp0%\path\to\target" %*
func ShimTarget(shim string) string {
	data, err := os.ReadFile(shim)
	if err != nil {
		return ""
	}
	hits := shimLine.FindAllStringSubmatch(string(data), -1)
	if len(hits) == 0 {
		return ""
	}
	parts := regexp.MustCompile(`[\\/]`).Split(hits[len(hits)-1][1], -1)
	target := filepath.Join(append([]string{filepath.Dir(shim)}, parts...)...)
	if _, err := os.Stat(target); err != nil {
		return ""
	}
	return target
}

var (
	cmdMeta       = regexp.MustCompile(`[()%!^"<>&|;, ]`)
	quoteRun      = regexp.MustCompile(`(\\*)"`)
	trailingSlash = regexp.MustCompile(`(\\+)$`)
)

// CmdQuote escapes one argument for a command line that cmd.exe parses (the
// cross-spawn algorithm). twice is for batch files, which parse it again.
func CmdQuote(arg string, twice bool) string {
	a := quoteRun.ReplaceAllString(arg, `$1$1\"`)
	a = trailingSlash.ReplaceAllString(a, "$1$1")
	a = cmdMeta.ReplaceAllString(`"`+a+`"`, "^$0")
	if twice {
		a = cmdMeta.ReplaceAllString(a, "^$0")
	}
	return a
}

// Cmd is a resolved command. CmdLine, when set, is the verbatim Windows
// command line (cmd.exe /d /s /c "..."); Args are then informational.
type Cmd struct {
	Path    string
	Args    []string
	CmdLine string
}

// Resolve returns what to start for `name args` on this OS.
func Resolve(name string, args []string, env Env) (Cmd, error) {
	return resolve(name, args, env, Windows)
}

// resolve returns plain `name` outside Windows; there the executable found on
// PATH. A .cmd shim is resolved to the program behind it, and only as a last
// resort run through cmd.exe, which cannot carry newlines or very long
// command lines.
func resolve(name string, args []string, env Env, win bool) (Cmd, error) {
	if !win {
		if p := LookPath(name, env, false); p != "" {
			return Cmd{Path: p, Args: args}, nil
		}
		return Cmd{Path: name, Args: args}, nil
	}
	found := LookPath(name, env, true)
	if found == "" {
		return Cmd{Path: name, Args: args}, nil
	}
	lower := strings.ToLower(found)
	if strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".com") {
		return Cmd{Path: found, Args: args}, nil
	}
	if target := ShimTarget(found); target != "" {
		t := strings.ToLower(target)
		if strings.HasSuffix(t, ".exe") {
			return Cmd{Path: target, Args: args}, nil
		}
		if strings.HasSuffix(t, ".js") || strings.HasSuffix(t, ".cjs") || strings.HasSuffix(t, ".mjs") {
			if node := LookPath("node", env, true); node != "" {
				return Cmd{Path: node, Args: append([]string{target}, args...)}, nil
			}
		}
	}
	for _, a := range args {
		if strings.ContainsAny(a, "\r\n") {
			return Cmd{}, fmt.Errorf("%s is a batch file, which cannot pass multi-line arguments; put %s.exe on PATH (the native installer provides it)", found, name)
		}
	}
	parts := []string{cmdMeta.ReplaceAllString(found, "^$0")}
	for _, a := range args {
		parts = append(parts, CmdQuote(a, true))
	}
	line := strings.Join(parts, " ")
	if len(line) > 8000 {
		return Cmd{}, fmt.Errorf("%s is a batch file and the command line is too long for cmd.exe; put %s.exe on PATH", found, name)
	}
	comspec := env.get("ComSpec", true)
	if comspec == "" {
		comspec = "cmd.exe"
	}
	return Cmd{Path: comspec, Args: []string{"/d", "/s", "/c", `"` + line + `"`}, CmdLine: `"` + comspec + `" /d /s /c "` + line + `"`}, nil
}

// Command returns an *exec.Cmd for c.
func (c Cmd) Command(ctx context.Context) *exec.Cmd {
	cmd := exec.CommandContext(ctx, c.Path, c.Args...)
	if c.CmdLine != "" {
		setCmdLine(cmd, c.CmdLine)
	}
	hideWindow(cmd)
	return cmd
}

// GitBash finds Git for Windows' bash, which Claude Code runs status line
// commands with on Windows.
func GitBash(env Env) string {
	candidates := []string{env.get("CLAUDE_CODE_GIT_BASH_PATH", true)}
	if git := LookPath("git", env, true); git != "" {
		d := filepath.Dir(git)
		candidates = append(candidates, filepath.Join(d, "..", "bin", "bash.exe"), filepath.Join(d, "..", "..", "bin", "bash.exe"))
	}
	for _, base := range []string{env.get("ProgramFiles", true), env.get("ProgramFiles(x86)", true), joinIf(env.get("LOCALAPPDATA", true), "Programs")} {
		if base != "" {
			candidates = append(candidates, filepath.Join(base, "Git", "bin", "bash.exe"))
		}
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
			return c
		}
	}
	return ""
}

func joinIf(dir, name string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, name)
}

// PowerShell finds PowerShell 7, else Windows PowerShell.
func PowerShell(env Env) string {
	candidates := []string{joinIf(env.get("ProgramFiles", true), filepath.Join("PowerShell", "7", "pwsh.exe")), LookPath("pwsh", env, true),
		joinIf(env.get("SystemRoot", true), filepath.Join("System32", "WindowsPowerShell", "v1.0", "powershell.exe")), LookPath("powershell", env, true)}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if st, err := os.Stat(c); err == nil && st.Mode().IsRegular() {
			return c
		}
	}
	return ""
}

// TildeHome is the directory a leading ~ names both in Git Bash ($HOME, else
// the user's profile) and in PowerShell (the profile): USERPROFILE, unless
// HOME names another directory. "" when they differ.
func (e Env) TildeHome() string {
	profile, home := e.get("USERPROFILE", true), e.get("HOME", true)
	norm := func(p string) string { return strings.TrimRight(strings.ReplaceAll(p, `\`, "/"), "/") }
	if profile == "" || home != "" && !strings.EqualFold(norm(home), norm(profile)) {
		return ""
	}
	return profile
}

// Shell returns how to run a shell command the way Claude Code runs status
// line commands: sh on Linux and macOS, Git Bash on Windows, PowerShell
// without Git (cmd.exe as a last resort).
func Shell(command string, env Env) Cmd {
	if !Windows {
		return Cmd{Path: "sh", Args: []string{"-c", command}}
	}
	if bash := GitBash(env); bash != "" {
		return Cmd{Path: bash, Args: []string{"-c", command}}
	}
	if ps := PowerShell(env); ps != "" {
		return Cmd{Path: ps, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", command}}
	}
	comspec := env.get("ComSpec", true)
	if comspec == "" {
		comspec = "cmd.exe"
	}
	return Cmd{Path: comspec, Args: []string{"/d", "/s", "/c", command}, CmdLine: `"` + comspec + `" /d /s /c "` + command + `"`}
}
