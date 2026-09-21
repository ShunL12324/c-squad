# C Squad

Run Claude Code and Codex as one team in your terminal. Tell Master what to do;
it recruits members, assigns tasks, and brings back their results. A tmux
workspace shows each member's terminal and a shared task board.

## Install

```sh
npm install -g csquad
csquad doctor
csquad new -s my-team --engine claude
```

Create a named team with `csquad new -s NAME`; `csquad start --name NAME`
remains compatible. Use `list` to find teams, `attach NAME` to enter a running
team, `resume NAME` to restore a stopped team, and `stop NAME` to stop it.

Use `--engine codex` for a Codex Master. You can also run `npx csquad`.

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

This package runs no install scripts and never edits your shell configuration,
so npm cannot enable completion for you. One command does it.

Load it into the **current shell** only:

```sh
source <(csquad completion zsh)     # bash: source <(csquad completion bash)
csquad completion fish | source     # fish
```

Install it **persistently**:

```sh
csquad completion install           # writes a script and prints any line to add
csquad completion status            # what is installed, and how to verify it
```

Fish needs nothing else, and neither does bash once the bash-completion v2
package is installed: it reads the target directory and supplies helpers the
generated script calls. For zsh, add the
printed `fpath=(...)` line to `~/.zshrc` above the command that runs
`compinit` — with Oh My Zsh, above `source $ZSH/oh-my-zsh.sh` — then open a new
terminal. If completion still does not appear, the cached index is stale: run
`rm -f ~/.zcompdump*` and open another terminal.

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

Stop teams before upgrading. Uninstalling preserves user configuration and
project recovery data. Use one installation channel to avoid conflicting
copies of `csquad` on PATH.

[Documentation and demo](https://github.com/ShunL12324/c-squad#readme) ·
[Installation and shell completion](https://github.com/ShunL12324/c-squad/blob/main/docs/usage.md)

MIT licensed. Independent of OpenAI and Anthropic.
