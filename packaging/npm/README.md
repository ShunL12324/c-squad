# C Squad

Run Claude Code and Codex as one team in your terminal. Tell Master what to do;
it recruits members, assigns tasks, and brings back their results. A tmux
workspace shows each member's terminal and a shared task board.

## Install

```sh
npm install -g csquad
csquad doctor
csquad start --name my-team --engine claude
```

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
