<p align="center">
  <img src="docs/assets/hero.png" alt="C Squad — Claude Code + Codex. One team. Your terminal." width="100%">
</p>

<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-2456bf?style=flat-square"></a>
  <img alt="Built with Go" src="https://img.shields.io/badge/built_with-Go-2456bf?style=flat-square">
  <img alt="Linux, macOS, WSL 2" src="https://img.shields.io/badge/platforms-Linux_%C2%B7_macOS_%C2%B7_WSL_2-52627a?style=flat-square">
</p>

<p align="center">
  <img src="docs/assets/icon.png" alt="C Squad icon" width="80"><br>
  <strong>A local development team of Claude Code and Codex agents, coordinated from one terminal.</strong><br>
  让 Claude Code 和 Codex 在终端里组成开发小队。
</p>

<p align="center">
  <a href="#get-started">Get started</a> ·
  <a href="#how-it-works">How it works</a> ·
  <a href="docs/install.md">Installation</a> ·
  <a href="CONTRIBUTING.md">Contributing</a>
</p>

---

You talk to the **Master**. It recruits agents, assigns work, coordinates reviews,
and brings questions and results back to you. Each member runs the real Claude
Code or Codex TUI in its own tmux session—you can switch in and inspect the work
at any time.

**C** stands for **Claude Code** and **Codex**. Mix both engines in one team, using
your existing native accounts and project configuration.

| Capability | What you get |
| --- | --- |
| **Mixed-engine teams** | Choose an engine, model, and responsibility for each member. |
| **Shared coordination** | A persistent task board, direct messages, broadcasts, and requests for help. |
| **Isolated development** | One Git worktree per code task, with a designated writing owner. |
| **Review before merge** | Review and test evidence tied to a commit; Master approves the merge. |
| **Inspectable sessions** | Native TUIs, a team roster in tmux, and shortcuts between members. |
| **Recoverable work** | Master exit cleans up the team; tasks, messages, and code remain for recovery. |

## Get started

> **Pre-release:** source installation is available now. The Homebrew and APT
> channels below require the first public tagged release and repository setup.

**Homebrew** — after the first release:

```sh
brew install ShunL12324/tap/csquad
```

**Ubuntu / Debian** — add the [APT source](docs/install.md#ubuntu--debian) once, then:

```sh
sudo apt install csquad
```

**From source** — Go 1.26+, tmux, and at least one native agent CLI:

```sh
make install
# Ensure ~/.local/bin is on PATH.
```

Sign in to Claude Code or Codex using its own CLI, then start in your project:

```sh
csquad doctor
csquad start --name my-team
```

Tell the Master what you want:

> Implement password reset. Have one developer build it, another agent review
> it, and a third test the failure cases. Ask me before merging.

C Squad injects its coordination instructions for you. There is no collaboration
skill or MCP server to install, and no automatic opening user message. Your
existing native MCP configuration stays under the engine's control.

## How it works

```text
You ↔ Master
         ├── Developer · Claude Code or Codex
         ├── Reviewer  · Claude Code or Codex
         └── Tester    · Claude Code or Codex
                  ↕
       Shared tasks, messages, and Git worktrees
```

Roles are descriptions you choose when recruiting members, not a fixed template
system. A member can handle both review and testing. Agents communicate through
the CLI; a SQLite ledger records tasks and messages, while native engine adapters
deliver notifications.

1. **Delegate.** Master creates a task and assigns members, or opens it for claiming.
2. **Collaborate.** Members report progress, exchange messages, and escalate blockers.
3. **Verify.** Review and test evidence refer to the exact candidate commit.
4. **Merge.** Master approves and performs a fast-forward merge after validation.

Each code task has a worktree from the start, even with a single developer. Only
the task owner writes code there; parallel implementation uses separate tasks.
Research tasks can run without Git. Code tasks require a repository with an
existing commit.

## Stay in control

| Action | Shortcut or command |
| --- | --- |
| Switch team members | `Alt+←` / `Alt+→` |
| Return to Master | `Ctrl-b 0` |
| Open a numbered member | `Ctrl-b 1` … `Ctrl-b 9` |
| Detach and leave the team running | `Ctrl-b d` |
| Start without taking over your terminal | `csquad start --detach` |
| Inspect progress | `csquad board` |
| Restart a member | `csquad member restart alice` |
| Stop the team | `csquad stop` |
| Recover an interrupted team | `csquad resume` |

Outside a team session, use `csquad --team /path/to/team COMMAND`; team state is
normally in `.csquad/teams/<name>/`. Team shortcuts do not modify `~/.tmux.conf`.
If your terminal captures Alt-arrow, use the numbered shortcuts.

Master process exit stops the team; detaching does not. Recovery preserves
worktrees and uncommitted changes. `resume --fresh` starts new native sessions
when old session history is unavailable. A machine shutdown cannot execute
cleanup immediately; the next start/resume checks for remaining state.

## Configure only what you need

The first configuration load creates `~/.config/csquad/config.toml` with detailed
comments. Run `csquad config` to inspect the effective values and configuration
path. An optional project `.csquad.toml` overrides user settings by field.

```sh
csquad start --engine claude --model opus
csquad member add alice --engine codex --role 'Backend developer' \
  --instructions 'Implement T1 and cover error paths with tests' --task T1
csquad member add reviewer --engine claude --role 'Code reviewer' --task T1
```

Environment overrides work without account profiles:

```sh
csquad start --env CODEX_HOME=/absolute/path/to/codex-config
```

Precedence: inherited environment → configuration `env` → `start --env` →
`member add --env`. Running teams retain their startup configuration. Member
limits are configurable; there is no conversation-turn cap. A member's
`generation` identifies its current process incarnation, not an iteration limit.

**Agents bypass native approval prompts by default.** Set
`bypass_permissions = false` to retain approvals. This does not log in, supply
account quota, or override organization policy. Project files such as untracked
MCP configuration are not automatically copied into worktrees.

## Help and completion

```sh
csquad --help
csquad task create --help
csquad member add --help

# Zsh
autoload -Uz compinit && compinit
source <(csquad completion zsh)

# Bash
source <(csquad completion bash)

# Fish
csquad completion fish | source
```

Completion includes commands, flags, and IDs from the current team. It reads the
ledger without waking agents. `csquad help request` is a team escalation command;
use `--help` or `csquad usage` for command documentation.

## Status and boundaries

- **Early-stage software.** Linux has been exercised with real Claude Code/Codex
  sessions. macOS and Linux ARM64 builds exist; macOS/WSL runtime acceptance is
  still pending. Native Windows is not supported.
- **Native engine compatibility matters.** Prior manual checks used tmux 3.4,
  Claude Code 2.1.276, and Codex 0.154.0. Engine updates may require adapter changes.
- **Messages can be retried.** Delivery is not exactly-once; a transport acceptance
  is different from an agent acknowledgement. Persistent failures appear on the board.
- **Local state stays local.** `.csquad/` holds recovery data and should not be
  committed. Use local disk for the SQLite ledger. A per-team runtime handles
  delivery and cleanup; no system service is installed.
- **Upgrades are explicit.** Stop running teams before replacing the CLI binary.

## Development

```sh
make fmt             # Format Go code
make check           # Formatting, lint, and race tests
make build           # Local binary
make snapshot        # Archives, source, and .deb packages; no publication
make test-packaging  # Signed APT install/upgrade/removal in disposable Ubuntu
```

Tests use standard Go `testing`, with real Git/tmux integration and fake engine
processes. They do not require model accounts. Ordinary pushes and PRs run no
GitHub builds. A stable tag such as `v0.1.0` triggers checks and release automation.

Read [Contributing](CONTRIBUTING.md), [Architecture](docs/architecture.md), or
[Releasing](docs/releasing.md) for the relevant workflow.

## License

[MIT](LICENSE) © 2026 ShunL12324. Personal and commercial use are welcome.

C Squad is an independent project, not affiliated with OpenAI or Anthropic.
