package config

import "github.com/pelletier/go-toml/v2"

const documentHeader = `# C-Squad configuration (TOML)
# User configuration: $XDG_CONFIG_HOME/csquad/config.toml; defaults to ~/.config/csquad/config.toml.
# CSQUAD_CONFIG selects another file. A project's .csquad.toml overrides user settings by field.
# Omitted fields use defaults. Teams save a startup snapshot; editing this file does not change an already running team.
# Claude Code / Codex manage native MCP and authentication.
# A profile is the only place launch settings live: engine, model, env and an optional command.
# Select one with start --profile NAME or member add NAME --profile NAME; the pointers below choose the default.
# Profiles never hold responsibilities; those come from member add --instructions.
# Profile names are plain names with no built-in meaning: a profile called master is
# not applied to Master unless master_profile or start --profile selects it.
# The two profiles below are written out on first run. Rename, edit or delete them freely,
# keeping the pointers in step. With an empty pointer, members start codex and Master starts claude with opus[1m].
# An optional env table selects the account, or any other variable, for members launched with this profile:
# [profiles.NAME.env]
# CODEX_HOME = "/absolute/path/to/codex-home"
# CLAUDE_CONFIG_DIR = "/absolute/path/to/claude-config"
# Precedence, lowest to highest: inherited environment -> this table. There is no shared table above it.
# Use absolute paths. Values do not expand ~, $HOME, or command substitutions. An empty value unsets the variable.
# CSQUAD_*, TMUX and TMUX_PANE are managed by C-Squad and cannot be overridden.
# An optional command table replaces running the engine by name, including probes and helper subcommands:
# [profiles.NAME.command]
# executable = "/absolute/path/to/claude-wrapper"
# args = ["--profile", "work"]
# To use a persistent alias from ~/.zshrc, select shell = "zsh" and its name as executable.
# shell = "bash" loads ~/.bashrc. Leave shell unset for direct executable/argv mode.
# Fixed args precede every generated argument. No shell parsing or expansion.
# Wrappers must preserve the selected engine's CLI/protocol.
# In TOML, keys after a [profiles.NAME] header belong to that table. Place top-level settings before them.

`

// Document encodes configuration values with field guidance for editing by hand.
// It is shared by first-run file creation and the config command; existing files
// are never rewritten just to update comments.
func Document(c Config) ([]byte, error) {
	b, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte(documentHeader), b...), nil
}
