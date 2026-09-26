# Task submissions and evidence

Every `task submit` returns a `submission` ID (for example, `T7-r1`), a
`submission_revision`, and an immutable `submission_summary`. Use the ID returned
by `task inspect` or `board`; treat it as an opaque selector scoped to that team.
Code submissions also return the exact candidate commit SHA.

For a concise current status, run `csquad task inspect T7 --output table`. It
shows state and owner, candidate/submission, workspace, shortened scope and
acceptance, blockers and gates, and only the latest evidence from each
member/category for the current submission and candidate. Failures lead the
bounded evidence list; omitted counts remain visible. Any shortened text and
older evidence are available with `csquad task inspect T7 --output json`.
The default JSON output and its fields remain unchanged for scripts.

An identical submission retry retains its ID and evidence. Changing the summary
or code candidate requires master to run `task reopen` first. Reopen clears the
current submission, candidate, approval, and evidence. The next submission gets
a new revision even when its summary and commit are unchanged. Progress reports
can change `progress` without changing the submitted summary.

Research and other non-code work can record formal evidence without a commit:

```sh
# Task owner submits the work.
csquad task submit T7 --summary 'Reviewed the proposal and recorded findings'
# A different participating reviewer records independent review.
csquad task evidence T7 --submission T7-r1 --kind review --passed true --summary 'Sources and conclusions verified'
# Master approves the submitted work.
csquad task approve T7
```

The evidence author must be a task participant or master. Authors cannot review
their own submission, but may record test results. Master may still approve
non-code work without evidence; neither review nor test evidence is mandatory.
If evidence exists, every member/category must have a passing latest result.
A failed result blocks approval until that same member records a passing result
for the same category, or master reopens the task and the owner resubmits.

Code evidence retains `--sha COMMIT` compatibility and still requires the exact
candidate SHA. Alternatively, `--submission ID` selects the current submission;
the recorded evidence includes both that ID and its exact candidate SHA. If both
selectors are supplied, both must match. Non-code evidence requires
`--submission` and rejects `--sha`. Stale submission IDs are always rejected.
A SHA alone cannot distinguish submissions of the same commit; use
`--submission` when revision fencing is needed.

Code approval still requires passing independent review and test evidence,
resolved blockers and gates, and the unchanged candidate commit. Merge retains
its candidate/target approval checks and clean-worktree requirements. New
evidence withdraws an existing approval and returns the task to review.

Saved ledgers without submission IDs are migrated deterministically on read and
persisted on the next successful update. Existing submitted tasks receive a
first revision, with their current progress as the submitted summary. Legacy
evidence matching the candidate is attached to that revision; evidence for other
commits stays ineligible. Later revisions never inherit unbound evidence.
External repository closures remain a separate workflow, not submissions.

## Automatic task reports

Routine progress, claims, ordinary milestones and individual passing evidence
stay in the ledger/UI and do not wake master. Use `task progress` for identity
checks, recovery confirmations and informational receipts; do not send a message
just to say “noted”. Explicit `message send` remains immediate for actionable
coordination, and `question request` remains immediate for blockers/decisions.

Master receives task reports when a candidate is submitted, an approval gate is
reached, evidence fails, or the current submission acquires passing review and
test evidence with no unresolved failure. Non-code approval still does not
require a review/test pair. Reporting does not change any approval rule.
Reports include the task ID/title/state/owner, immutable submission ID, candidate
SHA, submission conclusion, progress, blockers, open question IDs and the latest
evidence per member/category for the current submission and SHA. Each evidence
entry carries its one-based index into the task evidence list. Task descriptions,
acceptance/setup instructions, workspace paths and old evidence bodies are omitted.
Titles are capped at 120 characters, conclusion/progress at 480, and evidence
summaries/blockers at 160, with an explicit truncation marker. Lists show at most
12 evidence entries and 8 blockers/questions; omitted counts and the total current
failure count remain visible. Triggering failures come first, then other failures. Read `task inspect TASK` for the
current full record, `question list` for questions, and `board` for recent message
history.

A queued submission report is folded into a newer readiness/failure report.
Repeated submission calls do not create duplicate reports. An answered question,
approved gate, withdrawn candidate, corrected failure or obsolete readiness
notice becomes `superseded` rather than being injected later. The original
message remains in ledger history with its `report` reference (kind, submission,
question, milestone, or one-based evidence index). It is excluded from inbox
and cannot be retried or revived by restart. Reports refresh their task snapshot
just before delivery; already delivered native-engine input cannot be retracted,
so recipients must still consult current ledger state.

Routine messages do not require a model ACK. Successful transport stays `sent`
without a timeout alarm, and restart/resume does not requeue it just because no
ACK was recorded. `sent` means only that transport accepted the message; it does
not mean read, answered, or completed. Task progress, submissions and evidence
remain the source of truth for work. Master follows up only when a response or
action is needed and substantive progress is absent: inspect member/task state
first, then ask once if necessary. There is no fixed response deadline.

`message inbox` lists message records that have not been manually acknowledged
or superseded, including successfully sent messages. It is not an unread queue
or a list of unfinished tasks, and reading it does not mutate receipt status.
Manual `message ack` is still available to hide a record; a substantive
`message reply` acknowledges the original message as before. Neither operation
proves task completion. Do not send a reply merely to acknowledge a receipt.

Failed transport still uses the durable outbox and automatic backoff. Interrupted
`sending` attempts can be retried during recovery because their outcome is unknown.
Explicit `message retry MESSAGE` can requeue a sent team message intentionally. Stable `message send --request-id` and
`message reply` retain their idempotency. An optional ACK recorded before
transport prevents delivery. A native engine crash between transport acceptance
and the ledger commit can still cause a duplicate; check message ID, generation
and current candidate before acting, without requiring a routine ACK.

The `needs_attention` delivery state no longer exists. A ledger written before it
was removed is normalized once, when its version is raised: entries carrying either
exact built-in missing-ACK diagnostic become `sent` (prior attempt metadata
preserved), and every other cause is requeued as `pending` for another attempt.
Normalization is visible on read and persisted by the next ledger update. Existing
native-engine queued input cannot be withdrawn by this migration.

## Cancellation

Master stops a task with `csquad task cancel TASK --reason TEXT`; the reason is
required and is recorded with who cancelled and when. A ready, in-progress,
in-review or awaiting-merge task can be cancelled. A task that is preparing its
workspace or merging must finish, or be reconciled or aborted with
`task abort-merge`, first. Done and cancelled tasks are final.

Cancelled is not success. No operation changes a cancelled task afterwards, it
never satisfies a dependency (dependents stay blocked, and the command names
them so master can cancel or replace them), and it never shows the completion
mark. Its owner may take other work. The owner, participants, workspace, branch,
candidate and evidence stay as history, and `clean-worktree` keeps its workspace.
No member process is stopped.

Cancelling withdraws what still asks someone to act on the task: queued
submission, readiness, failure, milestone, recovery and stall notices and
undelivered assignment or availability notices are superseded, and open
questions about it are answered with the cancellation, which releases a member
waiting on master. The owner and participants each receive one notice to stop
work. Messages members wrote are left alone.

The task panel has two tabs: **Active** holds every unfinished phase, and
**Done/Cancelled** (**Closed** when the pane is narrow) holds finished and
cancelled tasks with distinct badges; a cancelled card shows its reason.

Do not downgrade csquad once a task has been cancelled. An older version treats
a cancelled task as unfinished: its owner stays occupied, its dependents cannot
be claimed, a resume may tell its members to continue work, and the first
ledger write drops the cancellation record. Upgrading replaces a runtime still
running from an older version; nothing protects a downgrade.

## Brief report (removed)

The task panel's **Brief report** button, its `b`/`r` keys and `task brief TASK`
are removed; ask master directly instead. Submission, readiness and failure
reports, task progress and `task submit` summaries are unaffected. A Brief
request recorded by an older version stays in the ledger for audit and is never
delivered, retried or recovered.

## Completion

The task panel marks a task **✓ Completed** only when the agent workflow has
accepted it; the user never has to click anything. A code task is marked once
master merged the candidate that carries passing independent review and test
evidence (`✓ Completed · merged SHA`). A task without a workspace is marked once
master approved it with no failing evidence (`✓ Completed · accepted by master`).
Ready, in-progress, blocked, in-review, awaiting-merge, merging and cancelled
tasks are never marked, whatever the author reports and whether or not the member has
stopped. A task closed on an external commit stays in Done without the mark and
keeps its "Closed externally · not merged" note.

Earlier versions showed **Awaiting user confirmation** with a **Confirm
completion** button, the `c` key and `csquad task confirm`. These are removed; the
command is no longer recognized. A ledger that holds a `user_confirmation`
record still loads, keeps the record, and shows it in the task details as a
legacy record. It has no effect on completion.

Member navigation also keeps the current client's panel widths and header height
when switching between existing or newly created members. Responsive visibility
still hides panels when the terminal is too narrow or short.

Native border drags are sampled from the source window before member navigation
applies any saved dimensions. Team mouse-release bindings also save the final
widths and header height directly inside tmux before a later viewport resize;
only border drags trigger this capture, and inherited mouse bindings remain.
tmux's `after-resize-pane` hook observes the initial
`resize-pane -M`, but subsequent mouse motion does not reliably invoke it. A
matching saved window size identifies an unchanged viewport; a changed viewport
still takes the responsive layout path before copying dimensions. Layout hooks
repair only their owning session. Navigation batches pane changes and avoids
reconfiguring unrelated members.

`TestNativeBorderDragAndSwitchLatency` uses four isolated tmux sessions and a real
PTY at 280×77. It sends twelve SGR mouse motions, releases the divider, immediately
clicks another member, checks round trips, then drags again and expands the
terminal before any navigation. A third fresh drag precedes Alt navigation.
Release must persist the actual final size, not the first motion. The original
implementation reproduced actual width 40 with saved width 28 and reset to 28 on the first click. Local eight-switch samples improved
from 672–835 ms to 60–163 ms after the fix; these are observed timings on the test
host, not a portable latency guarantee. The test logs latency rather than imposing
a machine-dependent performance threshold. Existing PTY tests separately cover
first-visit sizing, narrow windows, terminal resizing, two attached clients and
Tasks popup/toggle races.

## Reclaiming merged worktrees

After merging a code task, master should inspect its checkout and reclaim it when
safe. Cleanup is an explicit action, not a background timer or a merge side effect:

```sh
csquad task clean-worktree T12 --dry-run
csquad task clean-worktree T12
```

The compact result reports `eligible`, `removed`, `already_removed`, or `retained`
with a reason. Only a confirmed, completed merge qualifies. Modified, untracked,
and ignored files, unmerged commits, changed repository/branch identities,
symlinked paths, locked worktrees, and assume-unchanged/skip-worktree index entries
are retained. Worktrees referenced by a
member's working directory are retained even when that member is stopped, so
resume cannot inherit a deleted directory. Move the member to an appropriate
existing directory through the member lifecycle commands before trying again.
Do not force-delete a retained checkout; record the reason and resolve it first.

Cleanup preserves task records, evidence, the recorded workspace path and branch
refs. It removes the checkout through Git, and can be retried after an interruption.
The command also works from a human terminal after the team has stopped. It does
not remove the team ledger, logs, handoffs or other task worktrees.

Notification policy boundary: ordinary milestones and task progress do not emit automatic master reports. A failed evidence record on an already submitted candidate still emits a structured failure report; the runtime does not infer whether a failure can be fixed autonomously. Keep intermediate test runs/fixes in task progress, and use submission evidence for delivery review. English prompts guide contextual escalation; they do not implement semantic classification or automatic follow-up.
