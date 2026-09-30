# C Squad

Queue tasks as Claude Code background sessions. `csquad add` publishes a task;
a dispatcher starts it with `claude --bg` once one of the configured slots is
free. Watch and answer the sessions in Claude Code's agent view
(`claude agents`), or take a quick look with `csquad ls` and `csquad peek`.

## Install

```sh
npm install -g csquad
csquad completion install --shell zsh  # macOS: follow the printed loading step
csquad doctor
```

Supports macOS and Linux on x64 and arm64. On Windows, install and run inside
WSL 2. This package includes the native Go binaries: no compiler, install
scripts, or additional binary downloads are needed. Install and sign in to
Claude Code separately.

## Use

```sh
csquad add 'Add a --done filter to the list command, with tests'
csquad ls
csquad peek T1
csquad          # claude with the csquad console prompt appended
```

## Shell completion

npm does not register completion with your shell. `csquad completion install`
writes the script into a directory you own and prints any remaining step; it
never edits your shell configuration. For other shells, select `--shell bash`
or `--shell fish`. Bash requires bash-completion v2. `csquad completion status`
checks installed files.

## Update or remove

```sh
npm install -g csquad@latest
npm uninstall -g csquad
```

Uninstalling preserves user-installed completion files, the one-time hint marker
(`$XDG_STATE_HOME/csquad/npm-completion-notice-v1`, defaulting under
`~/.local/state`), the configuration and the task database. Use one
installation channel to avoid conflicting copies of `csquad` on PATH.

[Documentation](https://github.com/ShunL12324/c-squad#readme) ·
[Installation and shell completion](https://github.com/ShunL12324/c-squad/blob/main/docs/install.md)

MIT licensed. Independent of Anthropic.
