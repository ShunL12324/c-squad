# csquad

English | [简体中文](https://github.com/ShunL12324/c-squad/blob/main/README.zh-CN.md)

<p>
  <a href="https://www.npmjs.com/package/csquad"><img src="https://img.shields.io/npm/v/csquad?color=78e8af&label=npm" alt="npm version"></a>
  <a href="https://github.com/ShunL12324/c-squad/blob/main/LICENSE"><img src="https://img.shields.io/npm/l/csquad?color=78e8af" alt="MIT license"></a>
</p>

In [Claude Code](https://claude.com/claude-code) or Codex, type `/create-task` and the work in
plain words. The work runs in the background as task T1, T2, … in its own git worktree, with
dependencies, checks and merges. Your status line shows each task live.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-80-light.svg" alt="The status line with 4 tasks. T9, Install migration from 1.x, needs you, merge conflict, 1h35m. T4, readme demo recording, done, 290k tokens, 3m. T1, Codex app-server reconnect, working, 326k tokens, plus 4.1k, 7m. T7, Windows PATH resolution, fixing 2/3, 210k tokens, idle 3m, 31m.">
</picture>

- **Parallel work.** Each task has its own session, worktree and branch. You keep your conversation.
- **A live status line.** One row per task: the state, the tokens and the time.
- **Dependencies.** "after T1" holds a task until T1 is merged.
- **Checks and merges.** Codex, Claude or a test command checks the work. If you ask, csquad merges it.
- **Safe cleanup.** `/finish-task` removes a task only when its work is merged. It never deletes unmerged work.

## Install

```sh
npm i -g csquad
csquad install
```

Or run `npx csquad@latest install`. The installer adds five skills, and adds the task rows below
your own status line. Then start or restart `claude` in a git repository.

Requirements: Claude Code 2.1.260 or newer, git, and Node.js for npm. Codex is optional.
Linux, macOS or Windows.

## Use it

| You type | What happens |
| --- | --- |
| `/create-task add retry to the HTTP client` | T1 starts in the background. |
| `/create-task after T1, document the retry options` | T2 waits for T1, then starts. |
| `/create-task with codex: add request logging` | T3 runs as a Codex thread. |
| `/create-task add a circuit breaker, have codex review it, merge when it passes` | Codex reviews T4, T4 fixes the findings, and csquad merges it. |
| `/task-status` | Claude reports on each task. |
| `/message-task T2 use log/slog` | T2 gets one more instruction. |
| `/finish-task T1` | Claude merges T1, then removes its worktree, branch and session. |

Plain words also work: "create a task to add retry". Type `/watch-task T1` to get a message
when T1 finishes or needs input.

![Animation: the user types /create-task three times. T1 starts. T2 waits for T1. T3 runs on Codex. The rows show the state and the tokens. T1 becomes done. The user types /finish-task T1. Claude merges T1, the T1 row goes, and T2 starts.](https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/demo.gif)

<details>
<summary><b>Read the status line</b></summary>

A row has a glyph, the task ID, the engine (`✻` Claude Code, `⬡` Codex), the title, the state,
the tokens and the time in the state. The most urgent tasks come first.

| Glyph | State | What to do |
| --- | --- | --- |
| `◆` | `needs input` | The task asks a question. Answer in agent view, or tell Claude. |
| `◆` | `needs you · reason` | csquad stopped the task. Read the reason, then run `csquad retry T7`. |
| `✗` | `failed` | The session failed. Read it with `csquad peek T7`. |
| `✓` | `done`, `ready` | The work is done, or passed its check. Finish it. |
| `⇡` `◑` `◐` `●` | `merging`, `fixing 2/3`, `checking 1/3`, `working` | Wait. |
| `◇` | `idle` | The task waits. Send a message or finish it. |
| `◌` | `waiting T5` | The task starts when T5 is finished. |
| `■` | `stopped` | Send a message to start it again, or cancel it. |
| `□` | `gone` | The session was removed outside csquad. Run `csquad cancel T7`. |

`326k +4.1k` is the total tokens and the new tokens since the last refresh. `idle 3m` in amber
means no activity for 3 minutes. The rows show only the tasks of this conversation. After
`/clear`, run `csquad status --all`.

One task at 80 columns:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-1-task-80-light.svg" alt="The status line with one task. T4, readme demo recording, done, 290k tokens, 3m.">
</picture>

Nine tasks at 160 columns, in two columns. The last row lists the other tasks:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-160-light.svg" alt="The status line with 9 tasks in two columns. The last row shows plus 3 more, T7, T8 and T5.">
</picture>

Nine tasks at 124 columns. The titles are cut:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-9-tasks-124-light.svg" alt="The status line with 9 tasks in two columns. The titles are cut to 16 cells.">
</picture>

A long badge at 56 columns. The time stays in its column:

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-dark.svg">
  <img src="https://raw.githubusercontent.com/ShunL12324/c-squad/main/docs/images/ledger/ledger-long-badge-56-light.svg" alt="The status line at 56 columns with 3 tasks. The titles and the reason of the needs you badge are cut.">
</picture>

</details>

<details>
<summary><b>Command reference</b></summary>

Claude runs these commands for you. You can also run them in a shell: the binary is
`~/.csquad/bin/csquad`. `csquad COMMAND --help` shows the usage. Most commands accept `--all`
for a task of another conversation.

| Command | Description |
| --- | --- |
| `launch --title T [flags] [-- ENGINE_ARGS]` | Start a task. The prompt comes from stdin. |
| `status [--all] [--json]` | Show the tasks: state, age, session, latest message. |
| `peek T12 [-n 6]` | Show the last messages of a task. |
| `wait T12 [T13...] [--timeout MIN]` | Return when a task needs input, finishes, fails or is removed. |
| `send T12 "text"` | Send one more instruction. |
| `finish T12 [T13...] [--into BRANCH]` | Make sure the work is merged and clean, then remove the worktree, branches and session. |
| `cancel T12 [--discard]` | Stop a task and remove it from the list. Unmerged work stays, unless `--discard`. |
| `retry T12` | Continue a task that needs you, where it stopped. |
| `install`, `update`, `uninstall`, `status-install` | Manage the installation. |

Flags of `launch`:

| Flag | Description |
| --- | --- |
| `--after T3,T4` | Start when these tasks are finished. |
| `--check SPEC` | Check the work when the task stops: `codex[:MODEL]`, `claude[:MODEL]` or `cmd:COMMAND`. |
| `--max N` | The maximum number of check rounds. The default is 3. |
| `--merge [BRANCH]` | Merge the checked work. The default is the branch of the main checkout. |
| `--engine claude\|codex`, `--model M` | The engine and the model. The default is `claude`. |
| `--cwd DIR` | Run the task in DIR. |
| `--isolation none` | Edit the checkout in place: no worktree, no branch, no commit. |
| `--fork` | Start as a fork of this conversation. |
| `--prompt-file F [--rm-prompt]` | Read the prompt from F. |
| `-- ENGINE_ARGS` | Arguments for `claude --bg`, or Codex options such as `--effort high`. |

</details>

<details>
<summary><b>Configuration</b></summary>

**`~/.csquad/models.md`** tells Claude which engine, model and arguments to use for which work.
Claude reads it before each launch. csquad never writes it.

```markdown
- Small fixes, docs, tests: claude, `-- --model haiku`
- Features and refactors: claude, `-- --model opus --effort high`
- Reviews: `--check codex`

## When to use create-task

Also use it when I say "farm this out".
```

The `## When to use create-task` section adds your trigger phrases to the skill. Run
`csquad update` after you change it.

| Variable | Default | Use |
| --- | --- | --- |
| `CSQUAD_HOME` | `~/.csquad` | The binary, the task database, `models.md` and logs. |
| `CLAUDE_CONFIG_DIR` | `~/.claude` | Claude Code. The skills go into its `skills` directory. |
| `CODEX_HOME` | `~/.codex` | Codex. If it exists, `install` also sets up Codex. |
| `NO_COLOR` | not set | Remove the colors from the status line. |

A task uses the same provider as the session that created it: csquad copies the provider
variables (`ANTHROPIC_*`, `CLAUDE_CODE_USE_*` and others) into the settings of the task. For Codex, approve the
csquad Stop hook once with `/hooks`.

</details>

<details>
<summary><b>Update and uninstall</b></summary>

```sh
npx csquad@latest update
~/.csquad/bin/csquad uninstall
```

`uninstall` restores your status line and removes the skills, the binary and the Codex hook. It
keeps `~/.csquad/csquad.db` and `~/.csquad/models.md`. Running tasks continue.

**From csquad 1.x:** run `npx csquad@1 uninstall` first, then `npx csquad@latest install`.

</details>

<details>
<summary><b>FAQ</b></summary>

**The status line shows no task rows.**
Run `~/.csquad/bin/csquad status-install`. A `statusLine` in the project's
`.claude/settings.json` replaces the csquad status line in that project.

**A launch fails because the workspace is not trusted.**
Open `claude` in that directory once and accept the trust prompt.

**A task fails with "Login expired" or uses the wrong provider.**
Start `claude` from a shell that has your provider variables, then create the task again.

**I sent a message, but the task did nothing.**
Claude Code can hold a message for your approval. Open agent view (`claude agents`), select the
task, and approve the message.

**`csquad finish` refuses to remove a task.**
The work is not merged, or the worktree has changes. Merge or commit the work, then finish
again. `csquad cancel T7` keeps the work. `csquad cancel T7 --discard` deletes it.

**Does a task merge by itself?**
Only when you ask for it ("merge when it passes"). Otherwise, `/finish-task` merges it.

**How many tasks can run at the same time?**
csquad sets no limit. Each task uses its own tokens.

**`npm i -g csquad` installed no binary.**
The binary is an optional dependency. Install again without `--omit=optional`.

</details>

## Contributing

To build, test or release csquad, see
[CONTRIBUTING.md](https://github.com/ShunL12324/c-squad/blob/main/CONTRIBUTING.md).
MIT licensed, see [LICENSE](https://github.com/ShunL12324/c-squad/blob/main/LICENSE).
