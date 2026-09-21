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
Reports include the task state, owner/participants, immutable submission ID,
candidate SHA, submission conclusion, progress, blockers, open questions and
evidence with member/category/result references. Read `task inspect TASK` for the
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

Successful transport is not an agent ACK. A sent message is not automatically
injected again: after five minutes without ACK it becomes `needs_attention`,
visible through inbox/board. Inspect the recipient before explicitly running
`message retry MESSAGE`. Failed transport still uses the durable outbox and
automatic backoff. Recipient restart/resume can redeliver unacknowledged messages.
Stable `message send --request-id` and `message reply` retain their idempotency.
An ACK recorded before transport prevents delivery, including when a message
was first consumed through inbox. Transport and ledger commits cannot be atomic
across a native engine crash, so message IDs/ACK remain necessary for recovery.
