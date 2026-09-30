# Redesign: a task queue on top of Claude Code background sessions

## Why

The tmux-based team runtime made C Squad heavy: it owned processes, typed
messages into panes, ran its own delivery queue, detected stalls, recovered
sessions and drew its own UI. Members also reported to Master constantly and
Master replied, which filled the Master conversation with coordination noise.

Claude Code now provides most of that natively:

- `claude --bg` starts a background session with a prompt, a name, an agent and
  an appended system prompt.
- `claude agents` (agent view) lists every background session with its state
  (working, needs input, done, failed, stopped), a live one-line summary, a peek
  panel, replies without attaching, and notifications.
- `claude agents --json --all` exposes the same state for scripts.

C Squad keeps only what Claude Code does not have: a queue with a concurrency
limit, prompt injection for the sessions it starts, and a quick way to read a
session's recent conversation without switching to it.

## Principles

- **Pull, not push.** Workers never report to anyone. The user asks for status
  (`csquad ls`, `csquad peek`) when they want it. Messages the user sends to a
  worker need no reply.
- **No stored runtime state.** The database records what C Squad itself decided
  (task text, directory, launched session). Session state is always read live
  from `claude agents --json`, so there is nothing to synchronise.
- **Soft constraints in prompts, not code.** Worktrees, branches, commits,
  review and merging are prompt-level conventions. Claude Code already moves
  background sessions into `.claude/worktrees/` by default; if that is disabled
  or unavailable, work continues in place. C Squad never creates worktrees or
  merges; the one exception is cleanup of already merged work
  (`csquad finish`), which works with or without worktrees.
- **No roles in code.** There is no Master process and no planner. The user
  opens a session with `csquad` and publishes tasks from it, or runs
  `csquad add` from any shell. Specialised behaviour comes from Claude Code
  agents (`--agent reviewer`), defined in `.claude/agents/*.md`.
- **Claude Code only.** Codex support, tmux and the custom UI are removed.

## Commands

```text
csquad [-- CLAUDE_ARGS...]     Start an interactive Claude Code session with the
                               csquad console prompt appended
csquad add PROMPT...           Queue a task (--cwd, --agent, --name, --model,
                               --file FILE|-)
csquad ls [--all]              List tasks for this directory (or all) with live
                               session state and the last assistant line
csquad peek TASK [-n N]        Show the last N conversation messages of a task's
                               session, tool calls folded into one-line counts
                               (--tools to show them)
csquad finish TASK             Once its work is merged, remove the session, its
                               worktrees and branches (--into, --dry-run)
csquad cancel TASK             Remove a queued task that has not started
csquad dispatch                Run the dispatcher in the foreground (normally
                               started automatically by add)
csquad doctor                  Check claude availability, paths and dispatcher
csquad config                  Show the effective configuration
csquad version | update | completion
```

## Storage

One global SQLite database at `$XDG_DATA_HOME/csquad/csquad.db`
(default `~/.local/share/csquad/csquad.db`), in WAL mode with a busy timeout, so
concurrent `add` calls and the dispatcher need no extra locking.

```sql
CREATE TABLE tasks (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  prompt      TEXT NOT NULL,
  name        TEXT NOT NULL,
  cwd         TEXT NOT NULL,
  agent       TEXT NOT NULL DEFAULT '',
  model       TEXT NOT NULL DEFAULT '',
  state       TEXT NOT NULL,          -- queued | launched | cancelled | failed | finished
  session     TEXT NOT NULL DEFAULT '', -- Claude Code background session id
  error       TEXT NOT NULL DEFAULT '', -- launch failure
  created_at  INTEGER NOT NULL,
  launched_at INTEGER,
  ended_at    INTEGER                 -- cancelled, failed or finished
);
```

Tasks are shown as `T<id>`. One queue and one slot limit are shared by every
project, because quota is per account.

## Dispatcher

A single dispatcher process serves all projects. `csquad add` starts it
detached when it is not already running; a lock file in the data directory keeps
it single. Every few seconds it:

1. Reads `claude agents --json --all`.
2. Counts occupied slots: launched tasks whose session state is `working` or
   `blocked` (needs input). `done`, `failed`, `stopped` and sessions that no
   longer exist free their slot. A session launched moments ago that is not yet
   listed still counts as occupied.
3. Launches queued tasks in FIFO order while slots are free:
   `claude --bg --name NAME [--agent A] [--model M] [--permission-mode P]
   --append-system-prompt WORKER_PROMPT PROMPT` in the task directory, and stores
   the returned session id. A launch failure marks the task `failed` with the
   error text.
4. Exits when no task is queued. The next `add` starts it again.

A session that finished and later receives a message from the user becomes
`working` again and takes a slot again; the queue simply waits.

### Workspace trust

`claude --bg` refuses directories the user has not trusted. The dispatcher
marks the task directory trusted in Claude Code's global state
(`projects[DIR].hasTrustDialogAccepted` in `~/.claude.json`, or
`$CLAUDE_CONFIG_DIR/.claude.json`) before launching. This is an internal Claude
Code file, not a supported interface, and the user accepted that risk: the file
is rewritten atomically, but a concurrent write by a running Claude Code
process can still lose one side's change. The home directory is refused,
because Claude Code never persists trust for it.

## Reading a session

`peek` and the last-line column of `ls` read the session transcript
`$CLAUDE_CONFIG_DIR/projects/*/<session id>*.jsonl` (default `~/.claude`). The
lookup is by session id, because background sessions move into worktrees and the
project directory name is not predictable.

Kept: user text and assistant text. Folded into a count line such as
`· Bash ×3, Edit ×2`: tool calls. Dropped: tool results, thinking, meta entries,
sidechains, system reminders and command wrappers. Unknown entry types are
skipped, since the transcript format is internal and can change.

## Finishing a task

Prompt-level cleanup proved unreliable: `claude rm` refuses a worktree whose
commits exist on no remote even when they are merged locally, and a worker that
leaves its worktree with ExitWorktree detaches it from the session, so
`claude rm` no longer removes it. `csquad finish TASK` closes that gap without
making worktrees mandatory:

1. The session transcript records the `cwd` and `gitBranch` of every message,
   and the shell commands it ran. From these it collects the linked worktrees
   the session worked in, the branches checked out there or in the main
   checkout, and branches its commands created (`checkout -b`, `switch -c`,
   `worktree add -b`, `branch NAME`); a branch created and left within one
   command never appears as a location.
2. Every such branch except the target must be contained in the target
   (default: the branch checked out in the main checkout), and every worktree
   must have no uncommitted or untracked files. Otherwise it stops and lists
   the blockers.
3. It removes the session with `claude rm`, confirming the discard of
   "unpushed" commits it has just verified are merged, then removes worktrees
   that survived, deletes the branches, and marks the task finished.

A worker that edited the main checkout directly on the target branch leaves
nothing but its session. Outside git, only the session is removed.

`ls` marks launched tasks whose session was removed elsewhere as finished,
hides finished and cancelled tasks unless `--history` is given, and deletes
tasks that ended more than 7 days ago.

## Prompts

- **Console prompt** (appended to the `csquad` session): how to publish
  self-contained tasks with `csquad add`, how to check them with `ls`/`peek`,
  and that workers do not report back.
- **Worker prompt** (appended to every queued session): work autonomously,
  prefer an isolated worktree or branch, commit finished work, end with a short
  result (what changed, branch, commit, anything unresolved), decide reasonable
  questions yourself and ask the user only when genuinely blocked, never send
  status reports to other sessions, and never wait for replies to messages.
  Workers also clean up after themselves: stop processes they started and
  delete scratch files. They leave their branch and session worktree for
  review; `csquad finish` removes those once merged.

## Configuration

`$XDG_CONFIG_HOME/csquad/config.toml` (default `~/.config/csquad/config.toml`):

```toml
slots = 6                 # concurrent background sessions, 1..32
model = ""                # default --model for queued sessions
permission_mode = ""      # default --permission-mode for queued sessions
```

Unknown keys from the previous format are ignored.

## Removed

tmux runtime, team UI, members, messages, delivery, stall detection, recovery,
pinning, profiles, Codex, task worktrees, merge intents, evidence and gates, and
the `new`/`resume`/`attach`/`member`/`task`/`message`/`board` commands. Existing
team state directories are left on disk untouched; nothing migrates them.

## Deferred

Task dependencies, per-task stage pipelines, automatic review, and a system
service for the dispatcher. Add them only when real use needs them.
