---
name: create-task
description: Dispatch work to background Claude Code sessions or Codex threads ("tasks" named T1, T2, ...). Use when the user asks to create, publish, queue, dispatch or start a task, or to hand work to background agents.
---

# Create tasks

Each task runs as its own background session named `T12 · title`: a Claude
Code background session by default, which the user watches in agent view
(`claude agents`), or a Codex thread with `--engine codex` (for "run it in
codex"), which `codex resume THREAD` opens. Start one with the prompt on
stdin, in a quoted heredoc so nothing in it is expanded:

```bash
{{CSQUAD}} launch --title "retry sqlite busy on open" [--cwd DIR] [--engine claude|codex] [--model MODEL] [-- ENGINE_ARGS...] <<'PROMPT'
The full task prompt.
PROMPT
```

In PowerShell, pipe a here-string instead: `@'` on its own line, the prompt,
`'@` on its own line, then `| & {{CSQUAD}} launch --title "..."`.
<!-- claude -->
`--prompt-file FILE [--rm-prompt]` reads the prompt from a file instead.
<!-- /claude -->
<!-- codex -->
Pass the prompt this way only; do not write it to a file.

Run every csquad command outside Codex's sandbox (ask for escalated
permissions): it writes `~/.csquad` and talks to Codex's app-server, so
inside the sandbox it fails.
<!-- /codex -->

Before the first launch in a conversation, read `~/.csquad/models.md` if it
exists: it is the user's own routing (which engine, model or arguments for
which kind of work). Follow it, and pass a model it names explicitly with
`--model`.

Anything after `--` goes to the engine, for options the user asks for:

- Claude (default): passed to `claude --bg` unchanged, e.g.
  `-- --model opus --effort high` or `-- --settings '{"model":"sonnet"}'`. It
  overrides csquad's defaults (model, `--permission-mode bypassPermissions`).
  `--bg`, `--name`, `--print`, `--resume`, `--continue` and
  `--append-system-prompt` are set by csquad and are refused.
- Codex: thread options named as the `codex` command names them,
  `-m/--model M`, `--effort low|medium|high|xhigh`,
  `-s/--sandbox read-only|workspace-write|danger-full-access`,
  `-a/--ask-for-approval untrusted|on-request|never` and
  `-c/--config KEY=VALUE` (repeatable). Defaults: `danger-full-access` and
  `never`. Anything else is refused before the task starts.

If the user wants a task to edit the main checkout in place, for example to see
its changes live through a running dev server, add `--isolation none`: the
task then edits its working directory instead of a worktree, creates no
branches and leaves its changes uncommitted. Only one in-place task should work
on a checkout at a time. `--no-worker-rules` leaves out the worker rules, for a
prompt that brings its own.

It assigns the next task number, starts the session in `--cwd` (default: the
current directory) with the worker rules added to its instructions, and
prints `launched T12 · title · session ID · dir`. Tell the user the task IDs.

Order, checks and merging, only when the user asks for them:

- "T2 after T1", "start it when T3 and T4 are done": `--after T1` or
  `--after T3,T4` (existing tasks only). The task is queued
  (`queued T2 · ...`) and starts once all of them are done (merged, or
  finished with finish-task); its prompt then gets what they left (merged
  branch, final message). A task that is only ready does not release it.
- "let codex review it", "have claude check it", "at most 3 rounds":
  `--check codex`, `--check claude` (a model after a colon:
  `--check claude:opus`), `--max 3` (the default). A fresh read-only
  reviewer checks the work each time the worker stops; its findings go back
  to the worker until it passes or the rounds run out. "run the tests on
  it": `--check 'cmd:go test ./...'` (exit 0 passes; runs in the task's
  worktree).
- "merge when it passes", "merge it when done": `--merge` (into the branch
  checked out in the main checkout) or `--merge BRANCH`. csquad rebases the
  branch, fast-forwards the target and cleans up; on a conflict it asks the
  worker to rebase first. Not with `--isolation none`.

Never add `--after`, `--check` or `--merge` on your own: a task the user
dispatched in one sentence runs without them. A task that stops for the
user shows `needs you · reason`; `{{CSQUAD}} retry T12` resumes it where it
stopped once the user has looked.

Writing a task:

- The worker sees only its prompt, never this conversation. Make it
  self-contained: the goal, relevant files and context, constraints, and how to
  verify the result.
- Split independent work into separate tasks so they run in parallel. Keep work
  that edits the same code in one task, or say in each prompt how they relate.
- Queue what the user asked for; do not add review or test tasks they did not
  ask for.
- Title: 2–5 lowercase words naming the outcome. The ID is added for you.
- Run tasks in the project they belong to. If the user asks for work in another
  project, pass `--cwd` and say so.
- The user manages how many tasks run at once. If many are already working
  (`{{CSQUAD}} status --all`; use it only to count, never to report other
  sessions' tasks), mention it rather than holding tasks back yourself.

After launching, do not poll or wait: workers never report back. The user
<!-- claude -->
checks progress in agent view, in the status line, or by asking you
(task-status).
<!-- /claude -->
<!-- codex -->
checks progress by asking you (task-status) or with `{{CSQUAD}} status`.
<!-- /codex -->

Only if the user explicitly asks to be told when the tasks finish or need input
(e.g. "dispatch X and let me know when it's done"), or to accept them
yourself (e.g. "dispatch X and accept it yourself", "merge it when it's
done"), follow the watch-task skill for the tasks just launched; in the second
case it finishes them when done. Otherwise do not monitor.

If a Claude launch fails because the workspace is not trusted, ask the user to
open `claude` in that directory once and accept the trust prompt. If a Codex
launch cannot reach the app-server, csquad starts it
(`codex app-server daemon start`); if that fails too, show the user the
error.
