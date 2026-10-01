---
name: message-task
description: Send further instructions to a running or finished background task (T1, T2, ...). Use when the user wants to tell, ask, redirect or add something to a task.
---

# Message a task

Use Claude Code's built-in `SendMessage` tool; there is no command for this.
Address the session by its full name, `T12 · title`. `ListAgents` shows the
exact names, as does `node {{CSQUAD_DIR}}/tasks.js status`.

- Write the message so it stands on its own: what to change or add, and why if
  it matters. The worker treats it as the user's instruction.
- Send it once and do not wait for a reply. Workers act without answering, and
  a finished session resumes work.
- Tell the user it was sent. They can check the effect later (task-status or
  agent view).
- If the task is not listed (it was removed), say so; start a new task instead
  if the user wants.
