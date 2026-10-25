---
name: create-task
description: Dispatch work to background Claude Code sessions or Codex threads ("tasks" named T1, T2, ...). Use when the user asks to create, publish, queue, dispatch or start a task, or to hand work to background agents.
---

# Create tasks

A task is a background session named `T12 · title`: Claude Code by default,
or a Codex thread with `--engine codex`. It works in its own git worktree,
commits and cleans up after itself, and never reports back.

Before every launch, read `~/.csquad/models.md` if it exists: the user's
routing (which engine, model and arguments for which kind of work). Follow
it, and pass the model it names explicitly.

Launch with the prompt on stdin, in a quoted heredoc so nothing in it is
expanded. Write no draft files.

```bash
{{CSQUAD}} launch --title "retry sqlite busy" [--cwd DIR] [--engine codex] [--model M] [-- ENGINE_ARGS...] <<'PROMPT'
The full task prompt.
PROMPT
```

In PowerShell, pipe a here-string instead: `@'` on its own line, the
prompt, `'@` on its own line, then `| & {{CSQUAD}} launch --title "..."`.
<!-- codex -->

Run every csquad command outside Codex's sandbox (ask for escalated
permissions): it writes `~/.csquad` and talks to Codex's app-server.
<!-- /codex -->

It prints `launched T12 · title · session ID · dir`. Tell the user the task
IDs, then do not poll or wait.

Only when the user asks for them, never on your own:

- "T2 after T1": `--after T1`. Tell the user T1 will be merged
  automatically when it is done, then T2 starts. Only if they want to review
  T1 first ("wait for me to review T1", "don't merge it yet"): `--no-merge`
  on T1, or `{{CSQUAD}} hold T1` if T1 exists.
- "let codex review it", "at most 3 rounds": `--check codex` or
  `--check claude[:MODEL]`, `--max 3`; "run the tests on it":
  `--check 'cmd:go test ./...'`.
- "merge it when it passes": `--merge [BRANCH]`.
- "edit my checkout in place": `--isolation none`.
- "fork this conversation", "with what we discussed": `--fork`.

`reference.md` next to this file explains these flags, the engine arguments
after `--` and what to do when a launch fails. Read it when you need any of
that.

Writing a task:

- The worker sees only its prompt, never this conversation (unless
  `--fork`). Make it self-contained: the goal, relevant files and context,
  constraints, and how to verify the result.
- Split independent work into tasks that run in parallel; keep work on the
  same code in one task.
- Queue what the user asked for; add no review or test tasks of your own.
- Title: 2–5 lowercase words naming the outcome. The ID is added for you.
- Run tasks in the project they belong to; for another project pass `--cwd`
  and say so.

To tell a task more later, use message-task. Only if the user asks to be
told when the tasks finish or need input, or to accept them yourself
("merge it when it's done"), follow watch-task for the tasks just launched.
