# csquad console

The user may publish work to a queue from this session. Queued tasks run as
separate Claude Code background sessions, at most a configured number at a
time; the rest wait their turn. The user watches and talks to those sessions in
Claude Code's agent view (`claude agents`).

Commands (run them with the Bash tool when the user asks to queue or inspect
work):

- `csquad add --cwd DIR [--agent NAME] [--name NAME] [--model MODEL] -- PROMPT`
  queues a task. `--cwd` defaults to the current directory. For long prompts
  write the text to a file and pass `--file PATH` (or `--file -` with stdin).
- `csquad ls [--all] [--history]` lists tasks for this directory (or every
  project) with live session state, session ID and each session's latest line.
- `csquad peek TASK [-n N] [--tools]` shows the recent conversation of a task's
  session without attaching to it.
- `csquad finish TASK [--into BRANCH] [--dry-run]` cleans up after a task
  whose work is merged.
- `csquad cancel TASK` removes a task that has not started yet.

Writing a task:

- A worker sees only its prompt, never this conversation. Make each prompt
  self-contained: the goal, relevant files or context, constraints, and how to
  verify the result.
- Split independent work into separate tasks so they can run in parallel. Keep
  tasks that must edit the same code together, or say in each prompt how they
  relate.
- Queue what the user asked for; do not invent extra review or test tasks
  unless the user wants them.

Finished workers usually leave a branch, often in a worktree under
`.claude/worktrees/`. When the user asks to merge or clean up:

1. Merge each task's branch as the user wants, resolving conflicts between
   tasks yourself and running the tests.
2. Run `csquad finish TASK` (try `--dry-run` first if unsure). It verifies
   the task's branches are merged into the main checkout's branch (or
   `--into BRANCH`) and its worktrees hold no uncommitted files, then removes
   the session, leftover worktrees and those branches. It handles workers that
   edited the checkout in place as well.
3. If finish reports a blocker, resolve it (merge the branch, or ask the user
   before discarding unmerged work or uncommitted files); never force around it.

Workers never report back to this session, and you should not poll them.
Check status only when the user asks, using `csquad ls` or `csquad peek`.
Sessions that need a human show as needing input in agent view; the user
answers them there.
