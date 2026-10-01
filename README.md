# C Squad

Dispatch work to background [Claude Code](https://claude.com/claude-code) sessions
and keep an eye on them. Each task is a normal `claude --bg` session named
`T1 · title`, `T2 · title`, ... that you watch and answer in agent view
(`claude agents`) and see as a grid of chips in your status line.

C Squad is a handful of plain files, installed by one command:

- four **skills** that let Claude create, report on, message and finish tasks;
- `tasks.js`, the small script behind them (and the status line);
- `worker.md`, rules appended to every task's system prompt: work autonomously,
  prefer an isolated worktree, commit the result, report briefly at the end.

No daemon, no database, no runtime dependencies. Node 18+ on Linux or macOS.

## Install

```sh
npx csquad@latest install     # also: --install
npx csquad@latest update      # --update, after a new release
npx csquad@latest uninstall   # --uninstall
npx csquad@latest status      # what is installed, version, whether the status line is wrapped
```

Files go to `$CLAUDE_CONFIG_DIR` (default `~/.claude`):

```
csquad/tasks.js  csquad/worker.md  csquad/manifest.json  csquad/statusline.json
skills/{create-task,task-status,message-task,finish-task}/SKILL.md
```

The skills call `tasks.js` by absolute path, templated at install time from
`$CLAUDE_CONFIG_DIR`. Set the variable the same way when you update or uninstall.

Install and update are idempotent. `manifest.json` records the package version
and a hash of every installed file; if you edited one, the next install/update
saves your version as `<file>.bak`, says so, and then overwrites it.
`csquad/config.json` is yours and is never touched.

## The skills

Talk to Claude Code normally; the skills are picked up from what you ask.

| Skill | Say something like |
| --- | --- |
| `create-task` | "Create a task to retry on SQLite busy errors." "Queue three tasks: fix the login redirect, add tests for the parser, update the docs." |
| `task-status` | "How are my tasks going?" "Which tasks need input?" |
| `message-task` | "Tell T3 to also handle the empty-input case." "Ask T5 why it changed the schema." |
| `finish-task` | "Merge and clean up T2 and T4." "Finish everything that is done." |

`create-task` runs `tasks.js launch`, which numbers the task (one above the
highest T number Claude Code lists, under a lock so parallel launches never
collide), starts `claude --bg --name "T7 · title"` in the project directory,
appends the worker rules and prints `launched T7 · ...`. `finish-task` merges the task's branch and removes the
session and its worktree, only after checking the work is merged.

### Which tasks you see

The status line and `status` show only the tasks launched by the **current
session**: that `launched T7 · ...` line is recorded in the session transcript
and matched against the running tasks. `npx csquad@latest status` is about the
install; for tasks run `node <dir>/csquad/tasks.js status --all` (or ask
Claude "show all tasks") to list every task regardless of session.
`/clear` starts a new session, so tasks launched before it drop off the status
line; they still exist, and `status --all` shows them.

## The status line

Claude Code has one `statusLine` command. To add task chips without losing
yours, `install` wraps it:

1. If `settings.json` has a `statusLine` that is not ours, it is saved verbatim to
   `csquad/statusline.json` (or recorded as "none"). A one-time copy of the whole
   file goes to `csquad/settings.json.bak`.
2. Only the `statusLine` key changes, to
   `node <dir>/csquad/tasks.js statusline` with your `refreshInterval` (5 if you
   had none). Other keys, key order and indentation are preserved; the write is
   atomic.
3. At render time `tasks.js statusline` runs your saved command with the same
   stdin, prints its output first, then one chip per task (most urgent first:
   needs input, done, failed, working, stopped), laid out in columns by
   `$COLUMNS`.

Installing again never wraps twice. A project-level `.claude/settings.json` that
sets `statusLine` overrides the wrapper in that project; install and status warn
about it.

**To undo:** `npx csquad@latest uninstall` puts your original `statusLine` back
(or removes the key if you had none) and removes the files it installed. If you
changed `statusLine` since installing, it is left alone and uninstall says so.
Running task sessions are not touched. By hand: copy `statusLine` from
`csquad/statusline.json` (or `csquad/settings.json.bak`) back into `settings.json`.

## Configuration

`<claude dir>/csquad/config.json`, optional:

```json
{ "permissionMode": "bypassPermissions", "model": "" }
```

- `permissionMode`: passed as `--permission-mode` to every task. The default
  `bypassPermissions` lets workers act without prompting; set `"acceptEdits"`,
  `"plan"` or `""` (Claude Code's default) for something stricter.
- `model`: passed as `--model`; empty uses Claude Code's default. A task can
  override it with `--model`.

If a launch fails because the workspace is not trusted, open `claude` in that
directory once and accept the trust prompt.

## Requirements

Node 18+ and Claude Code with background sessions (`claude --bg`,
`claude agents`), on Linux or macOS.

## Upgrading from the Go CLI (0.x)

1.0 replaces the Go binary with this npm package. Homebrew and APT are
discontinued. Remove the old install first:

```sh
brew uninstall csquad            # Homebrew
sudo apt remove csquad           # APT
npm uninstall -g csquad          # an old global npm install would shadow npx
rm -rf ~/.local/share/csquad ~/.config/csquad   # old data and config
```

Then run `npx csquad@latest install`. Old `T<n>` sessions keep working.

## Development

```sh
npm test    # node:test, runs against temporary CLAUDE_CONFIG_DIRs and a fake `claude`
```

See [CONTRIBUTING.md](CONTRIBUTING.md). MIT licensed.
