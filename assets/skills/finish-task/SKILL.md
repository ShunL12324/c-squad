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

Which tasks: the ones the user names. If they name none ("finish", "merge
them", "wrap up"), finish every task of this session that is done or idle, one
after another, without asking which. Skip tasks that are still working or need
input, and list them at the end. If one task hits a blocker (conflict, failing
tests, unmerged or uncommitted work), stop on that task, report it, and carry
on with the others.

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
   (`git merge-base --is-ancestor`), a detached worktree's `HEAD` too, and its
   worktree has no uncommitted changes. Then, in this order, it removes the
   verified worktrees and merged branches with git, then the session
   (`claude rm`, or archiving a Codex task's thread), and deletes the task's
   settings file, which can hold a provider token. A Codex task's worktree is
   `<repo>-wt/t12-slug` next to the repository, on `task/t12-slug`.
4. If it refuses, it names what is unmerged or uncommitted: report that to the
   user and stop on that task. Never work around it by deleting branches or
   worktrees yourself.
5. If `claude rm` refused and csquad printed a value for `--discard-unpushed`
   or `--force-remove-worktree`, csquad already used it where the work was
   proven merged; otherwise it says the session is left. Do not invent such a
   value.

Hard rules: never delete unmerged branches or uncommitted work, never use
`--force`, `-D`, `--discard-unpushed` or `--force-remove-worktree` yourself;
on any doubt, report what you found and ask. Leave branches and worktrees the
task did not create alone. Report what was merged and removed.

<!-- claude -->
When deleting, use literal absolute paths: `rm -rf` on a path built from a
variable (`"$D"/*`) needs a human's approval even in bypass mode; if you must
use a variable, write `"${D:?}"`. In PowerShell, use
`Remove-Item -LiteralPath 'C:\full\path' -Recurse -Force`, again with a literal
path.
<!-- /claude -->
