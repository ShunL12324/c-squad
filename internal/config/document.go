package config

import "github.com/pelletier/go-toml/v2"

const documentHeader = `# C-Squad configuration (TOML)
# User configuration: $XDG_CONFIG_HOME/csquad/config.toml; defaults to ~/.config/csquad/config.toml.
# CSQUAD_CONFIG selects another file. A project's .csquad.toml overrides user settings by field.
# Omitted fields use defaults. Command-line options override their corresponding configuration fields.
# Teams save a startup snapshot; editing this file does not change an already running team.
# Role templates and account profiles are optional. Claude Code / Codex manage native MCP and authentication.
# In TOML, keys after [env] belong to that table. Place other top-level settings before [env].
# Optional engine commands (uncomment at the end of this file):
# [engine_commands.claude]
# executable = "/absolute/path/to/claude-wrapper"
# args = ["--profile", "work"]
# [engine_commands.codex]
# executable = "codex-alt"
# args = []
# To use a persistent alias from ~/.zshrc, select shell = "zsh" and its name as executable.
# shell = "bash" loads ~/.bashrc. Leave shell unset for direct executable/argv mode.
# Fixed args precede every generated argument, including probes and helper subcommands.
# No shell parsing or expansion. Wrappers must preserve the selected engine's CLI/protocol.

`

// Document encodes configuration values with field guidance for editing by hand.
// It is shared by first-run file creation and the config command; existing files
// are never rewritten just to update comments.
func Document(c Config) ([]byte, error) {
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte(documentHeader), b...), nil
}
