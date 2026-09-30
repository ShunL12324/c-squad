<p align="center">
  <a href="LICENSE"><img alt="License: MIT" src="https://img.shields.io/badge/license-MIT-287d58?style=flat-square"></a>
  <img alt="Built with Go" src="https://img.shields.io/badge/built_with-Go-287d58?style=flat-square">
  <img alt="Linux, macOS, WSL 2" src="https://img.shields.io/badge/platforms-Linux_%C2%B7_macOS_%C2%B7_WSL_2-52627a?style=flat-square">
</p>

# Queue work for Claude Code, a few sessions at a time

**C Squad is a small CLI that turns tasks into Claude Code background sessions.**
You publish tasks as fast as you think of them; a dispatcher starts each one as
a `claude --bg` session once a slot is free, so a dozen tasks run six or eight
at a time instead of all at once.

Everything else is Claude Code's own: you watch, answer and steer the sessions
in its agent view (`claude agents`). C Squad adds only what agent view lacks:

- **A queue with a concurrency limit** shared by every project.
- **Prompt injection** for the sessions it starts: workers work autonomously,
  prefer an isolated worktree, commit their result, and never send status
  reports.
- **A quick look without switching**: `csquad peek` prints a session's recent
  conversation with tool calls folded away.

There is no team, no Master process, no messaging between agents and no custom
UI. Workers do not report back; you check when you want to.

## Quick start

### 1. Install

**npm — macOS / Linux / WSL 2**

```sh
npm install -g csquad
csquad completion install --shell zsh   # macOS: run/save the printed loading line
```

Install [system dependencies](docs/install.md#npm) separately, or use Homebrew/APT
below to install them together with C Squad. You can also run `npx csquad`.

**macOS / Homebrew**

```sh
brew tap ShunL12324/c-squad https://github.com/ShunL12324/c-squad
brew install ShunL12324/c-squad/csquad
```

**Ubuntu / Debian / WSL 2** — [add the APT repository once](docs/install.md#ubuntu--debian), then:

```sh
sudo apt update
sudo apt install csquad
```

Homebrew and APT install a precompiled C Squad binary. You also need an
installed, signed-in **Claude Code** CLI.

Prefer a manual install? Download a package for your platform from
[Releases](https://github.com/ShunL12324/c-squad/releases/latest).
See [Installation](docs/install.md) for details. Native Windows is not supported;
use WSL 2 instead.

### 2. Queue tasks

```sh
cd /path/to/your/project
csquad doctor
csquad add 'Add a --done filter to the list command, with tests'
csquad add --agent reviewer -- 'Review the auth changes on branch feat/login'
csquad add --file big-task.md
```

Or start a console session and let Claude queue the work for you:

```sh
csquad                  # claude with the csquad console prompt appended
csquad -- --model opus  # arguments after -- go to claude
```

> Split the refactor plan into independent tasks and queue them.

A worker sees only its own prompt, so make each task self-contained.

### 3. Check on them

```sh
csquad ls           # tasks from this directory: state, age, latest message
csquad ls --all     # every project
csquad peek T3      # recent conversation of T3's session
csquad cancel T5    # drop a task that has not started
claude agents       # watch, answer and attach to sessions
```

`ls` shows each task as `queued`, `working`, `needs input`, `done`, `failed`,
`stopped` or `gone`. Sessions that need you also show up in agent view and
trigger Claude Code's notifications.

## How it works

- `csquad add` stores the task in `~/.local/share/csquad/csquad.db` and starts
  the dispatcher if it is not running.
- The dispatcher counts sessions that are working or waiting for input, launches
  queued tasks in order while slots are free, and exits when the queue is empty.
- Session state is read live from `claude agents --json`; nothing is synced.
- Before launching in a directory Claude Code has not trusted yet, csquad marks
  it trusted in `~/.claude.json`. That file is internal to Claude Code; see
  [the design](docs/redesign.md#workspace-trust) for the trade-off.

## Configuration

`~/.config/csquad/config.toml` (all optional):

```toml
slots = 6                 # concurrent sessions across all projects
model = ""                # default --model for queued sessions
permission_mode = ""      # default --permission-mode, e.g. "auto"
```

`csquad config` shows the effective values.

## Learn more

- [Installation](docs/install.md): package managers, archives and completion.
- [Design](docs/redesign.md): why C Squad is built this way, and what it
  deliberately leaves to Claude Code.
- [Contributing](CONTRIBUTING.md) and [Releasing](docs/releasing.md).

## Development

```sh
make fmt    # Format Go code
make check  # Formatting, lint, and race tests
make build  # Build a local binary
```

## License

[MIT](LICENSE) © 2026 ShunL12324. Personal and commercial use are welcome.

C Squad is an independent project, not affiliated with Anthropic.
