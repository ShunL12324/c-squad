package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// Command selects an executable and literal arguments prepended to every engine
// invocation. Shell is opt-in for persistent aliases, not a command template.
// It is configured in a profile's [profiles.NAME.command] table.
type Command struct {
	Shell      string   `json:"shell,omitempty" toml:"shell,omitempty" comment:"Optional interactive shell for a persistent alias: bash or zsh. Empty runs the executable directly."`
	Executable string   `json:"executable" toml:"executable" comment:"Executable name on C-Squad's PATH or an absolute path; in shell mode, a command/alias name. Empty uses the profile's engine name. Not a shell command string."`
	Args       []string `json:"args" toml:"args" comment:"Literal fixed arguments placed before C-Squad's generated arguments, including helper subcommands."`
}

var aliasName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.+-]*$`)

// Invocation prepares a direct command or an interactive-shell alias. Only the
// validated name enters shell source; all arguments use positional parameters.
// pathPrefix optionally puts the bound launcher before the rc-configured PATH.
// Job control stays off so helper cancellation reaches the entire process group.
func (c Command) Invocation(env map[string]string, pathPrefix string, args ...string) (string, []string, map[string]string) {
	argv := c.Arguments(args...)
	if c.Shell == "" {
		return c.Executable, argv, env
	}
	script, protected := shellEnvironment(env)
	if pathPrefix != "" {
		protected["CSQUAD_COMMAND_PATH_PREFIX"] = pathPrefix
		script += "export PATH=\"$CSQUAD_COMMAND_PATH_PREFIX${PATH:+:$PATH}\"\nunset CSQUAD_COMMAND_PATH_PREFIX\n"
	}
	script += "eval '" + c.Executable + " \"$@\"'"
	return c.Shell, append([]string{"-ic", script, "csquad-engine"}, argv...), protected
}

// ProbeInvocation checks only the named alias/command, without executing it.
func (c Command) ProbeInvocation(env map[string]string) (string, []string, map[string]string) {
	script, protected := shellEnvironment(env)
	script += `command -v -- "$1" >/dev/null`
	return c.Shell, []string{"-ic", script, "csquad-engine", c.Executable}, protected
}

// Preserve explicit overrides across rc files without exposing their values in
// argv or shell source. Alias-local assignments intentionally take precedence.
func shellEnvironment(env map[string]string) (string, map[string]string) {
	protected := agentenv.Merge(env)
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var script strings.Builder
	script.WriteString("set +m\n")
	for i, key := range keys {
		saved := fmt.Sprintf("CSQUAD_COMMAND_ENV_%d", i)
		protected[saved] = env[key]
		if env[key] == "" {
			fmt.Fprintf(&script, "unset %s\n", key)
		} else {
			fmt.Fprintf(&script, "export %s=\"${%s}\"\n", key, saved)
		}
		fmt.Fprintf(&script, "unset %s\n", saved)
	}
	return script.String(), protected
}

// Arguments copies the fixed prefix so composing one invocation cannot change the snapshot.
func (c Command) Arguments(args ...string) []string {
	return append(append([]string(nil), c.Args...), args...)
}

// validateCommand rejects values that cannot be passed as argv. Relative paths
// are ambiguous across member workspaces and recovery. The label names the table
// being checked, so a profile command and a legacy engine_commands entry
// migrating into one report the same rules against their own key. An empty
// engine leaves the executable required, as no native name can fill in.
func validateCommand(label string, engine Engine, command Command) error {
	name := command.Executable
	if command.Shell != "" {
		if command.Shell != "bash" && command.Shell != "zsh" {
			return fmt.Errorf("%s.shell must be bash or zsh", label)
		}
		if name == "" {
			name = string(engine)
		}
		if !aliasName.MatchString(name) {
			return fmt.Errorf("%s.executable: shell mode requires a command or alias name (letters, digits, _, ., +, -; starting with a letter or _)", label)
		}
	}
	if strings.ContainsRune(name, 0) || (name != "" && strings.TrimSpace(name) == "") {
		return fmt.Errorf("%s.executable must be a nonblank executable without NUL bytes", label)
	}
	if (strings.ContainsAny(name, `/\\`) || name == "." || name == "..") && !filepath.IsAbs(name) {
		return fmt.Errorf("%s.executable: use an absolute path or a command name on PATH", label)
	}
	for _, arg := range command.Args {
		if strings.ContainsRune(arg, 0) {
			return fmt.Errorf("%s.args must not contain NUL bytes", label)
		}
	}
	return nil
}
