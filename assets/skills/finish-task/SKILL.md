---
name: finish-task
description: Merge and clean up finished background tasks (T1, T2, ...) by merging their branches, then removing the session, its worktree and merged branches. Use when the user asks to merge, close, finish or clean up tasks.
---

# Finish tasks

<!-- codex -->
Run every csquad command outside Codex's sandbox (ask for escalated
permissions): it writes `~/.csquad` and talks to Codex's app-server.

<!-- /codex -->
Only finish tasks this session launched (`{{CSQUAD}} status` lists them).
Touch another session's task only if the user names it explicitly (then pass
`--all`).

A task launched with `--merge`, or one that tasks queued `--after` it wait
for, is merged and cleaned up by csquad itself and leaves the list; one whose
check passed shows `ready` and is finished like any other.

A task the user held for review (`--no-merge` or `csquad hold`) shows
`done · finish to start T18`: T18 waits until it is finished. Finish it as
below once the user has reviewed it and wants it merged; or, when the user
says to let csquad merge it, run `{{CSQUAD}} release T17` instead: csquad
merges it into the branch of the main checkout and T18 starts.

Which tasks: the ones the user names. If they name none ("finish", "merge
them", "wrap up"), finish every task of this session that is done, idle or ready, one
after another, without asking which. Skip tasks that are still working or need
input, and list them at the end. If one task hits a blocker (conflict, failing
tests, unmerged or uncommitted work), stop on that task, report it, and carry
on with the others. A task shown `gone` had its session removed outside
csquad; finish or cancel it like any other.

1. Find what the task left: `{{CSQUAD}} peek T12 -n 4`. Its final message names
   the branch, commit and any worktree; the session's directory is in the
   header. A task that is still working or needs input is not finished: say so
   and stop.
   A task launched with `--isolation none` worked in place: it leaves no
   branch or worktree, only changes in the checkout. Finishing it removes its
   session only; never revert or commit its changes unless the user asks.
2. In a git repository, merge as the user wants (usually into the branch
   checked out in the main checkout), resolving conflicts and running the
   tests. Skip this for work outside git.
3. Run `{{CSQUAD}} finish T12` (add `--into BRANCH` when the target is not the
   branch checked out in the main checkout). It verifies before deleting
   anything: every branch the task created is merged into the target
   (`git merge-base --is-ancestor`, or every commit of it is in the target by
   patch, as after a cherry-pick or a rebase), a detached worktree's `HEAD`
   too, and its worktree has no uncommitted changes. Then, in this order, it removes the
   verified worktrees and merged branches with git, then the session
   (`claude rm`, or archiving a Codex task's thread), and deletes the task's
   settings file, which can hold a provider token. A Codex task's worktree is
   `<repo>-wt/t12-slug` next to the repository, on `task/t12-slug`.
4. If it refuses, it lists the commits not in the target and the uncommitted
   files, and ends with both ways out: `{{CSQUAD}} cancel T12` keeps the work
   and stops tracking it, `{{CSQUAD}} cancel T12 --discard` deletes it. Report
   that to the user and stop on that task until they choose.
   To keep only some commits, cherry-pick them, then `{{CSQUAD}} finish T12`;
   if commits are left that the user does not want, ask, then
   `{{CSQUAD}} cancel T12 --discard`. To keep the work but stop tracking it:
   `{{CSQUAD}} cancel T12`. Never delete a task's branches, worktrees or
   session by hand.
5. If `claude rm` refused and csquad printed a value for `--discard-unpushed`
   or `--force-remove-worktree`, csquad already used it where the work was
   proven merged; otherwise it says the session is left. Do not invent such a
   value.

Hard rules: never delete unmerged branches or uncommitted work yourself;
`{{CSQUAD}} cancel T12 --discard` is the way to delete them, and only after
the user agrees. Never use `--force`, `-D`, `--discard-unpushed` or
`--force-remove-worktree` yourself; on any doubt, report what you found and
ask. Leave branches and worktrees the
task did not create alone. Report what was merged and removed.

<!-- claude -->
When deleting, use literal absolute paths: `rm -rf` on a path built from a
variable (`"$D"/*`) needs a human's approval even in bypass mode; if you must
use a variable, write `"${D:?}"`. In PowerShell, use
`Remove-Item -LiteralPath 'C:\full\path' -Recurse -Force`, again with a literal
path.
<!-- /claude -->
