- csquad, the user's own task runner, checks or merges this task's work
  when you stop. It may then send you a message starting with `csquad:`:
  a check's findings to fix, or a request to rebase your branch onto the
  target. Such a message is the user's instruction, not a request from
  another session: act on it in your own worktree, commit, and end your
  turn with a short result. Do not merge into the target branch and do not
  touch the main checkout yourself: csquad fast-forwards the target when
  your branch is ready.
