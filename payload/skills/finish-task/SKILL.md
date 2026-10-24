---
name: finish-task
description: Merge and clean up finished background tasks (T1, T2, ...) by merging their branches, then removing the session, its worktree and merged branches. Use when the user asks to merge, close, finish or clean up tasks.
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
   A task launched with `--isolation none` worked in place: it leaves no
   branch or worktree, only changes in the checkout. Finishing it means
   removing its session (step 5); never revert or commit its changes unless
   the user asks.
2. In a git repository, merge as the user wants (usually into the branch
   checked out in the main checkout), resolving conflicts and running the
   tests. Skip this for work outside git.
3. Verify before deleting anything:
   - `git merge-base --is-ancestor BRANCH TARGET` succeeds for every branch the
     task created, and for the worktree's `HEAD` if it is detached.
   - `git -C WORKTREE status --porcelain` is empty for its worktree.
4. Remove what the task left, before its session: for its worktree,
   `git worktree unlock PATH` (Claude Code locks the ones it creates), then
   `git worktree remove PATH`; then `git worktree prune`, and
   `git branch -d BRANCH` for each merged branch. Removing the merged worktree
   first means `claude rm` has no unpushed commits left to object to.
5. Remove the session: `claude rm SESSION_ID`. If it still refuses and prints a
   value for `--discard-unpushed` or `--force-remove-worktree`, rerun it with
   that value, but only for a worktree step 3 proved merged. If it prints no
   such value (Claude Code before 2.1.260 has neither flag), do not invent
   one: finish the cleanup with git as in step 4 and report that the session
   is left.
6. Run `node {{CSQUAD_DIR}}/tasks.js status`: it also deletes the removed
   task's settings file, which can hold the provider token it was launched
   with.

Hard rules: never delete unmerged branches or uncommitted work, never use
`--force`, `-D`, `--discard-unpushed` or `--force-remove-worktree` unless step 3
proved the work is merged; on any doubt, report what you found and ask. Leave
branches and worktrees the task did not create alone. Report what was merged and removed.

When deleting, use literal absolute paths: `rm -rf` on a path built from a
variable (`"$D"/*`) needs a human's approval even in bypass mode; if you must
use a variable, write `"${D:?}"`. In PowerShell, use
`Remove-Item -LiteralPath 'C:\full\path' -Recurse -Force`, again with a literal
path.
