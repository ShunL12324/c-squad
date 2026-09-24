# Agent instruction templates

C-Squad owns its English instruction text in `internal/prompts/templates/*.tmpl`.
The standard-library `text/template` renderer loads these files with `go:embed`;
there is no third-party template dependency, remote prompt service, or live
template reload. Changes ship with the executable.

`startup` renders the launch prompt used by both engines. `runtime` renders the
hook context used on session start and recovery. Both use the same `shared`
fragment: shell/engine guidance, technical constraints, message protocol,
communication policy, role-specific master/worker guidance, and recovery
handoff. The command reference belongs to startup and is split by role: workers
see the daily loop (task inspect, progress, milestone, submit, evidence,
message inbox/reply/send, question request, claim for open tasks), and master
additionally sees task and member administration. This is guidance only;
authorization is still enforced by code. Every other flag is one `--help`
away. Engine-specific transport notes are conditional; the permission and CLI
identity rules apply to both.

Each rule is stated once. A member with a known task runs `task inspect TASK`
first and uses `board` only for cross-task coordination, and evidence names the
current submission with `--submission ID` for code and non-code tasks alike. The
startup prompt is kept under a whitespace-word budget for a fixed fixture (Codex
worker 780, master 1200), checked by the template tests.

The renderer requires team, member, positive generation and a supported engine.
Responsibilities, working directory, handoff and colors are typed data fields. Member instructions are passed as values, never parsed as templates;
braces, quotation marks and code stay literal. They come only from that member's
own record, written by `member add --instructions`; no configured text is looked
up by member or profile name.
The built-in instructions are English. User-provided responsibilities and
paths retain their original language and contents rather than being translated.
This prevents template execution, not semantic prompt injection: responsibilities
remain the authorized system/developer context described by the CLI.

Launch renders once and uses the same result for the prompt file and native
engine configuration. Recovery resolves explicit and legacy responsibilities
with the same precedence as launch. Rendering errors return to the caller;
a failed runtime render does not mark that context as delivered.
`prompts.Revision` participates in the once-per-session hook marker. Bump it when
a policy change needs reinjection on the next eligible hook. Existing sessions
must actually run the new executable/hook to receive the new text.

Communication guidance asks agents to work autonomously within scope, keep
ordinary progress in the ledger, and send when another member needs to act,
decide, or avoid a mistaken wait. Developers and reviewers coordinate directly;
master handles decisions and cross-task coordination. System-delivered facts
do not need a second confirmation message. Genuine blockers and time-sensitive
risks still escalate, and exceptions remain a matter of judgment, not a quota.
Routine uncertainty is distinguished from missing authority or an actual blocker.

Master reviews the current candidate SHA and consolidates findings instead of
answering every update. Workers retain their ban on asking the human directly,
merging, or removing worktrees. Master is guided to inspect
`task clean-worktree TASK --dry-run` after merging and explicitly clean only
when safe, preserving ledger/evidence and branches. It must retain a worktree
with uncommitted, untracked or ignored files, or one still used by a member.
The cleanup command is a separate implementation; templates do not schedule it.

These are model instructions. Template tests verify rendering, role separation,
literal data, identity/gate/evidence constraints and shared recovery policy;
they do not prove reduced message counts or token usage. Runtime authorization,
delivery deduplication, queue consumption and task transitions remain enforced
by code. Observe actual conversations and wake reasons before attributing a
behavioral improvement to wording changes.


Routine ACKs are optional in both startup and recovery context. This explicitly
replaces the earlier instruction to acknowledge every message. Message IDs,
generation checks, current task/candidate inspection and substantive CLI replies
remain required where applicable. Master checks for substantive progress before
an actually needed follow-up; it does not use a fixed timeout or demand receipts
for every update. This policy changes the prompt revision so existing sessions
receive it through the next eligible hook running the new executable.

## Parallel roles and coordinated validation

Keep developers working on separate tasks in parallel. Developers implement their
assigned scope and report implementation readiness to master. Reviewers inspect
candidates and return consolidated findings; they do not take over implementation
or automatically run tests. Writing regression cases may be part of development;
executing them is a separate testing assignment.

For a feature spanning related modules (for example, modules one, two and
three), master waits until all those modules are implemented and integrated,
then schedules a combined test pass on that candidate. Do not test module one
in isolation and repeat the same suite whenever the next module arrives.
Independent features need not wait for unrelated work. Master assigns an
explicit scope and candidate to a developer or dedicated tester. For related
code tasks, owners integrate their branches into the same final candidate SHA
before submission. Run the combined validation once on that SHA and record its
results against each task submission; obtain all required scope reviews before
merging the batch. This reuses a tested candidate without bypassing per-task
evidence requirements. Members do not start test runs after every small edit. A submitted
candidate may still have pending tests: describe that honestly, and retain the
required passing review and test evidence before approval/merge. On failure,
master coordinates the correction and the affected retest scope. Reuse results
for the exact tested candidate rather than independently repeating the same run;
do not transfer a passing result to an untested SHA.

Arrange participants before handing off review or testing. Prefer completion
notifications to long fixed sleeps, and ignore superseded task notices without
sending another acknowledgment. These rules apply to startup and recovery via
the shared roles fragment; they guide behavior rather than enforcing a hard
role-based command restriction.
