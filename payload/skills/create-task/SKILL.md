---
name: create-task
description: Dispatch work to background Claude Code sessions ("tasks" named T1, T2, ...). Use when the user asks to create, publish, queue, dispatch or start a task, or to hand work to background agents.
---

# Create tasks

Each task runs as its own background Claude Code session, named `T12 · title`,
that the user watches in agent view (`claude agents`). Start one with:

```bash
node {{CSQUAD_DIR}}/tasks.js launch --title "retry sqlite busy on open" [--cwd DIR] [--model MODEL] <<'PROMPT'
...the task prompt...
PROMPT
```

It assigns the next task number, starts the session in `--cwd` (default: the
current directory) with the worker rules appended to its system prompt, and
prints `launched T12 · title · session ID · dir`. Tell the user the task IDs.

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
  (see `node {{CSQUAD_DIR}}/tasks.js status --all`; use it only to count, never
  to report other sessions' tasks), mention it rather than holding tasks back
  yourself.

After launching, do not poll or wait: workers never report back. The user
checks progress in agent view, in the status line, or by asking you
(task-status).

Only if the user explicitly asks to be told when the tasks finish or need input
(e.g. "dispatch X and let me know when it's done"), or to accept them
yourself (e.g. "dispatch X and accept it yourself", "merge it when it's
done"), follow the watch-task skill for the tasks just launched; in the second
case it finishes them when done. Otherwise do not monitor.

If the launch fails because the workspace is not trusted, ask the user to open
`claude` in that directory once and accept the trust prompt.
