---
name: message-task
description: Send further instructions to a running or finished background task (T1, T2, ...). Use when the user wants to tell, ask, redirect or add something to a task.
---

# Message a task

<!-- codex -->
Run every csquad command outside Codex's sandbox (ask for escalated
permissions): it writes `~/.csquad` and talks to Codex's app-server.

<!-- /codex -->
Run:

```bash
{{CSQUAD}} send T12 "the message"
```

This is the one way to message a task, whatever its engine. For a Claude
task it delivers the message to a live session's inbox, or wakes a stopped
one with it (`claude --bg --resume`, with the task's own options). A task
running in bypass-permissions mode may hold the message until the user
approves it in agent view; say so. For a Codex task it adds the message to
the running turn, or starts a new turn. For a long message, leave out the
text and pass it on stdin (a quoted heredoc, or a PowerShell here-string).

Message only this session's tasks, unless the user names a task from another
session explicitly (then pass `--all`).

- Write the message so it stands on its own: what to change or add, and why if
  it matters. The worker treats it as the user's instruction.
- Send it once and do not wait for a reply. Workers act without answering, and
  a finished session resumes work.
- Tell the user it was sent. They can check the effect later (task-status or
  agent view).
- If the task is not listed (it was removed), say so; start a new task instead
  if the user wants.
