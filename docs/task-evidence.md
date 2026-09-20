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
csquad task submit T7 --summary 'Reviewed the proposal and recorded findings'
csquad task evidence T7 --submission T7-r1 --kind review --passed true --summary 'Sources and conclusions verified'
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
