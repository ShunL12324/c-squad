---
name: finish-task
description: Merge and clean up finished background tasks (T1, T2, ...): merge their branches, then remove the session, its worktree and merged branches. Use when the user asks to merge, close, finish or clean up tasks.
---

# Finish tasks

Only finish tasks this session launched (`node {{CSQUAD_DIR}}/tasks.js status`
lists them). Touch another session's task only if the user names it
explicitly (peek it with `--all`).

Which tasks: the ones the user names. If they name none ("finish", "merge
them", "wrap up"), finish every task of this session that is done, one after
another, without asking which. Skip tasks that are still working or need
input, and list them at the end. If one task hits a blocker (conflict,
failing tests, unmerged or uncommitted work), stop on that task, report it,
and carry on with the others.

1. Find what the task left: `node {{CSQUAD_DIR}}/tasks.js peek T12 -n 4`.
   Its final message names the branch, commit and any worktree; the session's
   directory is shown in the header. A task that is still working or needs
   input is not finished: say so and stop.
2. In a git repository, merge as the user wants (usually into the branch
   checked out in the main checkout), resolving conflicts and running the
   tests. Skip this for work outside git.
3. Verify before deleting anything:
   - `git merge-base --is-ancestor BRANCH TARGET` succeeds for every branch the
     task created, and for the worktree's `HEAD` if it is detached.
   - `git -C WORKTREE status --porcelain` is empty for its worktree.
4. Remove the session: `claude rm SESSION_ID`. It also removes the worktree
   Claude Code created for it. If it refuses because commits were never pushed
   and step 3 showed they are merged, rerun it with the
   `--discard-unpushed TOKEN` it printed.
5. Remove what is left. `claude rm` with the token already deletes the
   worktree and its branch; check `git worktree list` and `git branch`. Then
   remove a worktree still present (`git worktree unlock PATH;
   git worktree remove PATH`), run `git worktree prune`, and
   `git branch -d BRANCH` each merged branch that still exists.

Hard rules: never delete unmerged branches or uncommitted work, never use
`--force`, `-D` or `--discard-unpushed` unless step 3 proved the work is merged;
on any doubt, report what you found and ask. Leave branches and worktrees the
task did not create alone. Report what was merged and removed.

When deleting, use literal absolute paths: `rm -rf` on a path built from a
variable (`"$D"/*`) needs a human's approval even in bypass mode; if you must
use a variable, write `"${D:?}"`.
