---
name: watch-task
description: Start monitoring background tasks when the user explicitly asks ("watch T12", "tell me when T12 is done", "monitor these tasks"), optionally finishing them when done if the user says so ("accept it yourself", "merge it when done"); stop when asked ("stop watching"). Never monitor on your own.
---

# Watch tasks

Only when the user explicitly asks to be told about specific tasks. Workers
never report back, so this is a plain background command, not polling by you.

## Start

Run, with the shell tool's (Bash or PowerShell) `run_in_background` option, naming only the tasks the
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
  task's last message (`peek T12 -n 1`). Then ask whether to finish it, unless
  the user authorized finishing (below). Never finish or merge otherwise.
- **failed**, **stopped**, **removed**: one sentence on why.
- **still working after …**: say so and ask whether to keep watching.

Then, if other tasks the user asked to watch are still working, run `wait`
again for exactly those. Do nothing else unprompted.

## Finishing on its own, only when authorized

If the user explicitly told you to accept or finish these tasks yourself
("accept it yourself", "finish it when done", "merge it when it's done",
"自行验收"), then on **done** follow the finish-task skill for that task right
away instead of asking. Its rules still apply: on a conflict, failing tests,
unmerged or uncommitted work, or any doubt, stop and ask the user. Afterwards
report in a few lines what was merged and removed. The authorization covers
only the tasks it was given for.

## Stop

When the user says to stop watching, kill the background command with the
harness's stop/kill tool for background shells.
