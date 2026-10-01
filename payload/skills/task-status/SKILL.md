---
name: task-status
description: Report the progress of background tasks (T1, T2, ...). Use when the user asks how tasks are going, what is done, which need input, or for a task status report.
---

# Task status

Gather the facts:

```bash
node {{CSQUAD_DIR}}/tasks.js status          # tasks launched by this session
node {{CSQUAD_DIR}}/tasks.js status --all    # every task
node {{CSQUAD_DIR}}/tasks.js peek T12 -n 8   # recent conversation of one task
```

`status` lists the tasks launched by this session (`--all`: every task) with their
state (needs input, failed, done, working, stopped), age, session, directory and its latest message. `peek` shows the last messages
without tool calls.

Report in this order, briefly:

1. **Needs input**: the question the task is waiting on (peek it if the latest
   message does not make it clear), so the user can answer in agent view.
2. **Failed**: what went wrong.
3. **Done**: the result in a line or two (branch, verification, anything left
   unresolved), and that it is ready to merge and finish.
4. **Working**: one line each on what it is doing now.

Do not invent progress the messages do not show. Run this only when the user
asks; do not poll.
