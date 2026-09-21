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
type Command struct {
	Shell      string   `json:"shell,omitempty" toml:"shell,omitempty" comment:"Optional interactive shell for a persistent alias: bash or zsh. Empty runs the executable directly."`
	Executable string   `json:"executable" toml:"executable" comment:"Executable name on C-Squad's PATH or an absolute path; in shell mode, a command/alias name. Empty uses the engine name. Not a shell command string."`
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
		if key == "CLAUDE_CONFIG_DIR" && env[key] == "" {
			script.WriteString("unset CLAUDE_CONFIG_DIR\n")
		} else {
			fmt.Fprintf(&script, "export %s=\"${%s}\"\n", key, saved)
		}
		fmt.Fprintf(&script, "unset %s\n", saved)
	}
	return script.String(), protected
}

// Command returns the configured invocation, with the native executable as fallback.
func (c Config) Command(engine Engine) Command {
	command := c.EngineCommands[engine]
	if command.Executable == "" {
		command.Executable = string(engine)
	}
	return command
}

// Arguments copies the fixed prefix so composing one invocation cannot change the snapshot.
func (c Command) Arguments(args ...string) []string {
	return append(append([]string(nil), c.Args...), args...)
}

// ValidateCommands rejects unsupported engines and values that cannot be passed
// as argv. Relative paths are ambiguous across member workspaces and recovery.
func (c Config) ValidateCommands() error {
	for engine, command := range c.EngineCommands {
		if err := engine.Validate(); err != nil {
			return fmt.Errorf("engine_commands: %w", err)
		}
		name := command.Executable
		if command.Shell != "" {
			if command.Shell != "bash" && command.Shell != "zsh" {
				return fmt.Errorf("engine_commands.%s.shell must be bash or zsh", engine)
			}
			if name == "" {
				name = string(engine)
			}
			if !aliasName.MatchString(name) {
				return fmt.Errorf("engine_commands.%s.executable: shell mode requires a command or alias name (letters, digits, _, ., +, -; starting with a letter or _)", engine)
			}
		}
		if strings.ContainsRune(name, 0) || (name != "" && strings.TrimSpace(name) == "") {
			return fmt.Errorf("engine_commands.%s.executable must be a nonblank executable without NUL bytes", engine)
		}
		if (strings.ContainsAny(name, `/\\`) || name == "." || name == "..") && !filepath.IsAbs(name) {
			return fmt.Errorf("engine_commands.%s.executable: use an absolute path or a command name on PATH", engine)
		}
		for _, arg := range command.Args {
			if strings.ContainsRune(arg, 0) {
				return fmt.Errorf("engine_commands.%s.args must not contain NUL bytes", engine)
			}
		}
	}
	return nil
}
