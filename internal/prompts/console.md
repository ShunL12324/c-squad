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
- `csquad ls [--all]` lists tasks for this directory (or every project) with
  live session state and each session's latest line.
- `csquad peek TASK [-n N] [--tools]` shows the recent conversation of a task's
  session without attaching to it.
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

Finished workers usually leave a branch in a worktree under
`.claude/worktrees/`, owned by their session (the SESSION column of
`csquad ls`). When the user asks to merge or clean up:

1. Merge the task's branch as the user wants, resolving conflicts between
   tasks yourself and running the tests.
2. Confirm the branch is contained in the target:
   `git merge-base --is-ancestor BRANCH main`.
3. Remove the session with `claude rm SESSION`. That also removes its worktree
   and branch. It refuses when the commits are on no remote; once step 2
   passed they are safe, so rerun it with the `--discard-unpushed TOKEN` it
   prints. Do not delete a session's worktree with `git worktree remove`
   while the session still exists.
4. Check `git worktree list` and `git branch` afterwards. A worktree the
   worker left with ExitWorktree survives `claude rm`; once its branch is
   merged, remove it with `git worktree remove` and delete the branch.
5. Never discard a branch that is not merged, or a worktree with uncommitted
   changes, without asking the user.

Workers never report back to this session, and you should not poll them.
Check status only when the user asks, using `csquad ls` or `csquad peek`.
Sessions that need a human show as needing input in agent view; the user
answers them there.
