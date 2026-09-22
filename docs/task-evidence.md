# Task submissions and evidence

Every `task submit` returns a `submission` ID (for example, `T7-r1`), a
`submission_revision`, and an immutable `submission_summary`. Use the ID returned
by `task inspect` or `board`; treat it as an opaque selector scoped to that team.
Code submissions also return the exact candidate commit SHA.

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
history. The existing task-card Brief report action requests a human-readable
explanation from master.

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

## Brief: direct user input

The **Brief report** button, `b` shortcut, and `task brief TASK` send a short
English user prompt to master's native session: current progress, remaining
work, and blockers, with a direct reply in the user's language. Its template
lives in `internal/prompts/templates/brief.tmpl`. No task or team message record
is written: there is no team message ID, sender envelope, ACK, or outbox retry.
Codex uses its native thread queue through the configured executable/alias;
Claude receives a plain native user frame scoped to its current session ID.
Neither path types into the terminal composer. A missing native session,
unavailable master, or stale caller generation fails explicitly.

The panel suppresses rapid repeat clicks and concurrent requests. After feedback,
each deliberate click is a new question, including after failure. Success means
only that native transport completed, not that master read or answered it;
feedback is local to the panel and disappears on respawn. Transport failures
have no automatic retry; the user may click again. A lost transport response
can leave acceptance uncertain, so a manual repeat can duplicate native input.
Old Brief messages remain available for audit but no longer drive panel status,
automatic delivery/recovery, or explicit message retry. Input already accepted
by an engine cannot be withdrawn.

## User confirmation

After technical delivery, task cards show **Awaiting user confirmation**.
The user can request a **Brief report**, then click **Confirm completion** (or press
`c` on the selected task). A human terminal can also run `csquad task confirm TASK`.
This also works after the team has stopped, without restarting its runtime.
Agent generations cannot invoke that command to impersonate user acceptance.

Confirmation is optional: technically finished tasks remain in Done and release
the owner's execution slot even before user acceptance. The separate
`user_confirmation` record retains the user, timestamp, submission, candidate and
technical phase. Repeated clicks are idempotent. Confirmation never creates test
or review evidence, changes approval, merges code, or advances the technical phase.
A task whose technical ledger is still unfinished must first be reconciled by
master (for example, using the documented external closure workflow); confirmation
is not a substitute for that evidence.

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
