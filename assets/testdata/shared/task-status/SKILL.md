---
name: task-status
description: Report the progress of background tasks (T1, T2, ...). Use when the user asks how tasks are going, what is done, which need input, or for a task status report.
---

# Task status

**Codex:** Run every csquad command outside Codex's sandbox (ask for escalated
permissions): it writes `~/.csquad` and talks to Codex's app-server.

Gather the facts:

```bash
{{CSQUAD}} status          # tasks launched by this session
{{CSQUAD}} status --all    # every task
{{CSQUAD}} peek T12 -n 8   # recent conversation of one task
```

Report only this session's tasks; use `--all` (and `peek --all` for the others)
only when the user explicitly asks about all or other tasks.

`status` lists the tasks launched by this session (`--all`: every task) with
their state (needs input, failed, done, idle, working, stopped, gone), age,
session, directory and latest message. Tasks with a check, a merge or an
order show csquad's flow instead: `waiting T3`, `checking 1/3`,
`fixing 2/3`, `merging`, `ready` (the check passed), `needs you` with a
`note:` line saying why (`csquad retry T12` resumes it). A task that others
wait for is merged by csquad when its work is complete, unless the user
holds it for review: then it shows `done · finish to start T18` and the task
waiting for it `waiting T17 · review`, each with a `next:` line. T18 starts
only when the user has reviewed T17 and either runs `csquad release T17`
(csquad merges it) or merges it and finishes it (finish-task). A `gone` row means
the session was removed outside csquad, and some of its work (a worktree or
branch) is left; `csquad cancel T12` clears it and keeps unmerged work. A gone
task with nothing left leaves the list by itself. `status` cuts a long
latest message; `peek` shows the last messages with tool calls folded, the
last reply in full (`peek T12 -n 1`: the whole final report).

Report in this order, briefly:

1. **Needs input** or **needs you**: the question the task is waiting on
   (peek it if the latest message does not make it clear), so the user can
   answer it (in agent view for a Claude task, `codex resume THREAD` for a
   Codex task, or through message-task); for needs you, the note.
2. **Failed**: what went wrong.
3. **Done or idle**: the result in a line or two (branch, verification,
   anything left unresolved), and that it is ready to merge and finish. For
   `done · finish to start T18`, say that T18 waits for the user's review
   of it.
4. **Working**: one line each on what it is doing now. `status` also shows
   its token usage and last activity; mention a long quiet spell.

Do not invent progress the messages do not show. Run this only when the user
asks; do not poll.
