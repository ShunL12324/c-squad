---
name: message-task
description: Send further instructions to a running or finished background task (T1, T2, ...). Use when the user wants to tell, ask, redirect or add something to a task.
---

# Message a task

<!-- claude -->
From a Claude Code session, a Claude task can be reached with Claude Code's
built-in `SendMessage` tool: address the session by its full name,
`T12 · title` (`ListAgents` shows the exact names, as does
`{{CSQUAD}} status`). For a Codex task, or when SendMessage cannot reach it,
run:
<!-- /claude -->
<!-- codex -->
Run, outside Codex's sandbox (ask for escalated permissions):
<!-- /codex -->

```bash
{{CSQUAD}} send T12 "the message"
```

For a Claude task it delivers the message to a live session's inbox, or wakes
a stopped one with it (`claude --bg --resume`, with the task's own options). A
task running in bypass-permissions mode may hold a message from `csquad send`
until the user approves it in agent view; say so if you used it. For a Codex
task it adds the message to the running turn, or starts a new turn.

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
