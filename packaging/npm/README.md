# C Squad

Run Claude Code and Codex as one team in your terminal. Tell Master what to do;
it recruits members, assigns tasks, and brings back their results. A tmux
workspace shows each member's terminal and a shared task board.

## Install

```sh
npm install -g csquad
csquad completion install --shell zsh  # macOS: follow the printed loading step
csquad doctor
csquad new -s my-team
```

Create a named team with `csquad new -s NAME`; `csquad start --name NAME`
remains compatible. Use `list` to find teams, `attach NAME` to enter a running
team, `resume NAME` to restore a stopped team, and `stop NAME` to stop it.

Set `master_profile`, or pass `--profile NAME`, if you want another engine, model
or account as Master. You can also run `npx csquad`.

Supports macOS and Linux on x64 and arm64. On Windows, install and run inside
WSL 2. This package includes the native Go binaries: no compiler, install
scripts, or additional binary downloads are needed.

Install system dependencies first:

```sh
# macOS
brew install tmux git

# Ubuntu / Debian / WSL 2
sudo apt install tmux procps git
```

Install and sign in to Claude Code or Codex separately. Agents use your native
engine configuration and accounts. By default, C Squad bypasses native agent
permission prompts; this is configurable.

## Shell completion

npm links the executable but does not register completions in your shell. The
first interactive npm invocation prints a one-time setup hint; automated calls,
completion requests and agent panes stay quiet. No install script or startup
file is changed.

For **macOS + Zsh**, run:

```sh
csquad completion install --shell zsh
```

Run the exact loading line it prints in your **current Zsh**, and add that same
line at the **end of `~/.zshrc`**, after Oh My Zsh or other completion setup. It
initializes `compinit` only when needed and sources the installed script directly,
so an old `.zcompdump` or a missing `fpath` entry does not prevent registration.
The command only writes the script: it cannot change an already-open parent shell.
Open a new terminal to verify persistent setup.

For other shells, select `--shell bash` or `--shell fish` and follow its printed
instructions. Bash requires bash-completion v2. `csquad completion status` checks
installed files; it does not prove a running shell has loaded them.

In that new shell, `csquad sta` + **Tab** expands to `csquad start`; in zsh,
`print -r -- ${_comps[csquad]:-missing}` prints `_csquad` once completion is
registered.

The installed script resolves `csquad` from `PATH` when you press Tab and lives
in your own data directory rather than the Node prefix, so it keeps working
after `nvm use`, a Node upgrade, or `npm install -g csquad@latest`.

## Update or remove

```sh
npm install -g csquad@latest
npm uninstall -g csquad
```

Stop teams before upgrading. Uninstalling preserves user-installed completion files, the one-time hint marker
(`$XDG_STATE_HOME/csquad/npm-completion-notice-v1`, defaulting under `~/.local/state`),
user configuration and
project recovery data. Use one installation channel to avoid conflicting
copies of `csquad` on PATH.

[Documentation and demo](https://github.com/ShunL12324/c-squad#readme) ·
[Installation and shell completion](https://github.com/ShunL12324/c-squad/blob/main/docs/usage.md)

MIT licensed. Independent of OpenAI and Anthropic.
