<p align="center">
  <img src="docs/assets/banner.png" alt="csquad: a squad of background tasks for Claude Code" width="100%">
</p>

<p align="center">
  <a href="https://www.npmjs.com/package/csquad"><img src="https://img.shields.io/npm/v/csquad?color=78e8af&label=npm" alt="npm version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/npm/l/csquad?color=78e8af" alt="MIT license"></a>
</p>

**Hand work to a squad of background [Claude Code](https://claude.com/claude-code) sessions, and watch them from your status line.**

You keep talking to one normal `claude` session. Ask for a few tasks and each
becomes its own background session, `T1 · title`, `T2 · title`, ..., working in
its own git worktree while you carry on. One static binary, no daemon.

## Quick start

```sh
npx csquad@latest install
```

csquad 3 is a single Go binary; npm only delivers it. The `csquad` package
pulls in the binary for your platform through an optional dependency (such as
`@shunl12324/csquad-linux-x64`; nothing is downloaded by an install script),
and `install` copies it to `~/.csquad/bin/csquad`, so the skills and the status
line never run from the npx cache. Node.js is needed for `npx` only.

From source instead (Go 1.26+, no C toolchain needed):

```sh
git clone https://github.com/ShunL12324/c-squad.git && cd c-squad
CGO_ENABLED=0 go build -o csquad ./cmd/csquad
./csquad install
```

Then, in any `claude` session:

> Dispatch 3 tasks: fix the login redirect, add tests for the parser, update the docs.

That's it.

## How it works

```text
you     Dispatch 3 tasks: fix the login redirect, add tests for the parser, update the docs.
claude  launched T1 · fix login redirect, T2 · add parser tests, T3 · update the docs.
        They run in the background; check agent view or the status line.
```

Your status line now lists the tasks, most urgent first:

```text
▎ ◆ T2 add parser tests ··························  needs input        18m
▎ ✓ T1 fix login redirect ························  done        326k 1h05m
▎ ● T3 update the docs ···························  working ▇▆·▇▆▅▃▆    7m
```

```text
you     How are my tasks going?
claude  T2 needs input: "Should the parser reject empty files or return []?" Answer it in
        agent view, or tell me and I'll pass it on. T1 is done: fixed the redirect loop on
        /login, tests pass, branch worktree-t1-fix-login-redirect. T3 is still working.
you     Tell T2 to return an empty list. Then merge T1.
claude  Sent to T2. T1 merged into main; its session, worktree and branch are removed.
```

Each task is a plain `claude --bg` session with a small set of worker rules
appended to its system prompt: work autonomously, use an isolated worktree and
branch, clean up after itself, commit, and end with a short result. Workers never
report back on their own, and nothing merges until you ask.

### Order, checks and merging

Only when you ask for them, a task gets a flow of its own:

```text
you     T2 after T1. Let codex review T1, at most 3 rounds, and merge it when it passes.
```

- `--after T1,T3`: queued until those tasks are done (merged, or finished);
  it then starts with what they left (merged branch, final message).
- `--check codex|claude[:MODEL]|cmd:COMMAND` and `--max N` (default 3): when
  the worker stops, a fresh read-only reviewer (or the command, in the task's
  worktree) checks the work; failed findings go back to the worker, up to N
  rounds.
- `--merge [BRANCH]`: csquad rebases the branch onto the target (or asks the
  worker to, on a conflict), fast-forwards the target, verifies it with
  `git merge-base --is-ancestor`, cleans up and releases the queued tasks.

The status line shows `waiting T1`, `checking 1/3`, `fixing 2/3`, `merging`,
`ready`, or `needs you · reason` when a task stopped for you;
`csquad retry T12` resumes it where it stopped. Every phase change is a line
in `~/.csquad/logs/events.jsonl`. There is no service: each `csquad` command,
the tasks' Stop hooks and the status line move the flow along.

## The skills

Just ask; Claude picks the skill from what you say.

| Skill | What it does | Say something like |
| --- | --- | --- |
| `create-task` | Starts one background session per task, numbered `T1`, `T2`, ... | "Create a task to retry on SQLite busy errors." "Queue three tasks: ..." |
| `task-status` | Reports state and latest message of each task: needs input, failed, done, working | "How are my tasks going?" "Which tasks need input?" |
| `message-task` | Sends follow-up instructions to a task with `csquad send`, for any engine | "Tell T3 to also handle the empty-input case." |
| `watch-task` | Opt-in: runs a background command that checks the named tasks every 10 s and tells you when one needs input, finishes, fails or disappears. Never runs unless you ask | "Watch T3." "Tell me when T2 is done." "Stop watching." |
| `finish-task` | Merges a done task, then removes its session, worktree and branch, only after checking the work is merged | "Merge and clean up T2 and T4." "Finish everything that is done." |

## The status line

Your own status line stays exactly as it is; csquad prints it first, then one row
per task: its state, id and title, its token usage, and how long it has been in
that state.

| State | Meaning |
| --- | --- |
| `◆ needs input` | the task is waiting on an answer; reply in agent view |
| `✗ failed` | the session failed |
| `✓ done` | finished; ask Claude to merge it |
| `● working` | still running |
| `◇ idle` | finished its turn and waiting |
| `■ stopped` | stopped before finishing |

The usage cell answers "is it moving, and how hard". A working task (also
fixing or merging) shows a trail of its last 8 refreshes (about 80 s): one cell
per refresh, newest on the right, on a log scale from `▁` (under 500 tokens)
to `█` (32k or more); `·` means nothing changed, `▪` that the session changed
but its gateway reports no token counts. After 2 minutes without activity the
trail turns into dots and the idle time in amber (`····· 3m`); that is only a
look, the task's state does not change. Other tasks show their total, faint
(`326k`, `1.2M`, `?` when unknown). Fresh work counts input, cache writes and
output; cache reads are never counted. A Claude task's numbers come from its
transcript, a Codex task's from its rollout file; the status line keeps 9
readings per task in `~/.csquad/cache`. `csquad status` shows the full block
for each working task:

```text
## T365 · cancel smoke
state: working · started 27s ago · session ca47ac83 · ~/project
usage: 51.8k fresh · +0 since the last refresh (0 s) · last activity 13s ago
       input 4 · cache write 51.1k · output 622  cache read 50.5k (not counted)
       context 26% (51k of 200k) · last 20 s █·
```

Wide terminals show two to four tasks side by side (from 124, 187 and 250
columns; each cell is at least 60 wide). Below about 52 columns a cell drops
its usage trail step by step after the title has shrunk. At most four rows are used; when more tasks run, the last row lists
the rest as `+N more`. `NO_COLOR` turns colours off and keeps bold, faint and
reverse video.

The rows show only the tasks launched by the **current session**. `/clear`
starts a new session, so earlier tasks drop off the line but keep running; the
full list is always in agent view (`claude agents`), or ask Claude to
"show all tasks".

`install` wraps your existing `statusLine` instead of replacing it: the original
command is saved to `~/.csquad/statusline.json` and run at render time with the
same input. Other `settings.json` keys, ordering and indentation are untouched.
`uninstall` restores the original `statusLine` byte for byte (or removes the key
if you had none). A project-level `.claude/settings.json` that sets `statusLine`
overrides the wrapper in that project; `install` and `status` warn about it.

## Install, update, uninstall

```sh
npx csquad@latest install   # copy the binary to ~/.csquad/bin, install the skills, wrap the status line
npx csquad@latest update    # the same, for an existing installation
csquad update               # the installed copy of an npm install runs npx csquad@latest update
csquad uninstall            # restore the status line, remove what install wrote
csquad status-install       # what is installed and whether the status line is wrapped
```

`install` does not change your `PATH`: `csquad` above is
`~/.csquad/bin/csquad` (add `~/.csquad/bin` to `PATH` to type it short), or
`npx csquad@latest` in its place. `npx csquad@3 install` stays on 3.x. With
optional dependencies turned off (`--omit=optional`) npm installs no binary,
and the `csquad` command says so. A build from source updates by running
`update` from the newer build.

The skills go to `$CLAUDE_CONFIG_DIR/skills` (default `~/.claude`), everything
else to `$CSQUAD_HOME` (default `~/.csquad`):

```text
bin/csquad        the binary the skills and the status line run (absolute path)
csquad.db         the task ledger (SQLite, one table)
models.md         your model routing, optional, never written by csquad
statusline.json   your original status line
settings/T12.json a task's session settings while it runs (mode 600)
prompts/T12.md    a prompt too large for the command line
cache/            the status line's last output and each task's usage readings
logs/             check logs, events.jsonl, advance.log (rotated past 1 MiB)
```

Install and update are idempotent. A skill can be several files (create-task is
a short `SKILL.md` plus a `reference.md` it reads when needed); if you edited
one, the next run saves your version as `.bak` next to it and says so. Uninstall keeps
`csquad.db` and `models.md`; running task sessions are not affected.

A csquad 1.x installation (`~/.claude/csquad/`) is not migrated: install stops
and asks you to run `npx csquad@1 uninstall` first, which restores your own
status line.

## Configuration

Model routing lives in `~/.csquad/models.md`, a short file you own: which
engine, model or arguments for which kind of work. create-task reads it before
every launch, so keep it a short lookup table and name model aliases
explicitly (without one the engine's default model runs). Its `## When to use create-task` section (extra trigger phrases)
is appended to the installed skill's description by install and update.

For one task, ask Claude for any `claude` option ("use opus with high effort
for this one"): create-task passes everything after `--` in
`csquad launch … -- --model opus --effort high` to `claude --bg` unchanged,
overriding csquad's defaults (`--permission-mode bypassPermissions`).

Tasks normally work in their own worktree. Ask for one that edits your checkout
in place ("edit the main checkout so I can see it live") and create-task adds
`--isolation none`: Claude Code lets that task edit its working directory, and
it leaves its changes uncommitted for you.

A task uses the same provider as the session that launched it. Claude Code's
background service does not see your shell environment, so `ANTHROPIC_*`,
`CLAUDE_CODE_USE_*`, `CLAUDE_CODE_OAUTH_TOKEN` and `CLAUDE_CODE_SUBAGENT_MODEL`
from the launching session go into the task's settings file
(`~/.csquad/settings/T12.json`, mode 600, removed once the task is finished or
gone), never onto the command line or into your own `settings.json`. They are
forwarded all or nothing: when the task's `--settings`, your settings or the
project's choose a provider themselves, none are.

A task's worktree branches from your local HEAD (`worktree.baseRef: "head"`),
so it sees commits you have not pushed yet. Set `worktree.baseRef` yourself
(in your settings, the project's, or after `--`) to choose otherwise.

To hand over the conversation you are in ("fork this into a task, with what we
just discussed"), create-task adds `--fork`: the task starts as a fork of your
session and inherits its conversation (Claude Code:
`claude --bg --resume SESSION_ID --fork-session`; Codex: `thread/fork`). It
stays in the engine of the session you dispatch from, and its first turn reads
the whole inherited context again.

If a launch fails because the workspace is not trusted, open `claude` in that
directory once and accept the trust prompt.

## Codex

"Run it in codex" makes create-task add `--engine codex`: the task is then a
Codex thread named `T12 · title` on Codex's shared app-server daemon (csquad
starts it with `codex app-server daemon start` when it is not running).
`codex resume THREAD` opens it; `csquad status`, `peek`, `send`, `wait`,
`finish` and `cancel` work as for Claude tasks. The thread runs with the
worker rules as developer instructions, `danger-full-access` and approval
`never` by default; it creates its own worktree,
`<repo>-wt/t12-slug` on `task/t12-slug`, which `finish` finds by that name.
After `--` a Codex task takes thread options: `-m/--model`, `--effort`,
`-s/--sandbox`, `-a/--ask-for-approval` and `-c/--config KEY=VALUE`.

Codex sessions can dispatch too: when `$CODEX_HOME` (default `~/.codex`)
exists, install also puts the skills into `~/.agents/skills`, and tasks
launched from a Codex thread belong to it (`CODEX_THREAD_ID`). Codex must run
`csquad` outside its sandbox. Install also adds one Stop hook group, marked
`"__csquad__": true`, to `~/.codex/hooks.json` (approve it once with `/hooks`
in Codex); uninstall removes exactly the text it inserted. Codex has no status line, so there are no task
chips there.

## Requirements

- Claude Code with background sessions (`claude --bg`, `claude agents`);
  2.1.260 or newer for `claude rm --discard-unpushed`, which `csquad finish`
  uses for a worktree it verified merged (`install` and `status-install` warn
  on older versions)
- For Codex tasks: codex-cli with the app-server daemon (verified with
  0.163.0)
- Linux, macOS or Windows (amd64, arm64). On Windows, Claude Code runs status
  line commands through Git Bash, so csquad does too for your original status
  line (without Git it falls back to `cmd.exe`); a native `claude.exe` or the
  npm `claude.cmd` both work. Windows builds and vets but has not been run yet.

## Development

```sh
go vet ./... && go test ./...   # temporary homes and a fake `claude`, never the real ones
```

See [CONTRIBUTING.md](CONTRIBUTING.md). MIT licensed.
