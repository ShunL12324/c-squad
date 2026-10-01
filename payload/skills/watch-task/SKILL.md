---
name: watch-task
description: Start monitoring background tasks when the user explicitly asks ("watch T12", "tell me when T12 is done", "monitor these tasks"); stop when asked ("stop watching"). Never monitor on your own.
---

# Watch tasks

Only when the user explicitly asks to be told about specific tasks. Workers
never report back, so this is a plain background command, not polling by you.

## Start

Run, with the Bash tool's `run_in_background` option, naming only the tasks the
user asked about:

```bash
node {{CSQUAD_DIR}}/tasks.js wait T12 T13
```

For "watch my tasks", name this session's tasks that are still working (see
`node {{CSQUAD_DIR}}/tasks.js status`). The command checks every 10 seconds
without using the model, and exits when a named task needs input, finishes,
fails, is stopped or is removed, or after 4 hours. Pass `--timeout MINUTES` to
change that.

## When the background command completes

Read its output: one line per task, `T12 · title · done`. Act on each line with
as few tokens as possible:

- **needs input**: `node {{CSQUAD_DIR}}/tasks.js peek T12 -n 2`, then relay the
  task's exact question. The user can answer in agent view, or ask you to send
  it with message-task.
- **done** (or `already done`): one or two sentences on the result, from the
  task's last message (`peek T12 -n 1`). Then ask whether to finish it. Never
  finish or merge on your own.
- **failed**, **stopped**, **removed**: one sentence on why.
- **still working after …**: say so and ask whether to keep watching.

Then, if other tasks the user asked to watch are still working, run `wait`
again for exactly those. Do nothing else unprompted.

## Stop

When the user says to stop watching, kill the background command with the
harness's stop/kill tool for background shells.
