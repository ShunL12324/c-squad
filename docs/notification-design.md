# Notification delivery and concise handoffs

The September 2026 conversation audit found several different sources of noise,
not one transport duplication bug:

* A readiness message before submission can be necessary: master must arrange
  review or testing. A second summary after `task submit` or `task evidence`
  repeats the automatic report. For example, T602's M622 supplied an actionable
  review handoff, whereas its submission already generated M630.
* Monitoring updates can become obsolete before they reach the recipient. The
  T565 sequence M570/M577 described real intermediate registry failures, but
  M578 corrected them after reading the completed verifier log. Those are not
  independent unresolved release failures. Use one terminal outcome unless an
  actual decision or time-sensitive failure requires intervention.
* Master can amplify traffic by issuing review, readiness confirmation and
  evidence requests separately. Consolidating those instructions reduces both
  directions of the exchange. Answered Q609/Q614 notices later arrived as
  M610/M615; they must not restart resolved work or trigger another explanation.
* Assignment also needs valid member scope. Q609 was caused by assigning T606
  to a member whose injected role still allowed only T29. Notification batching
  does not fix incompatible roles; inspect them before assigning new work.
* Automatic submission reports repeated identical conclusion and progress
  fields. The formatter now omits the redundant progress field.

These examples establish redundant content and obsolete delivery, not a measured
percentage of execution time. Ordinary `task progress` already stays in the
ledger; it is not automatically injected into master on every update.

## Native Codex transport

The source comparison pinned official `openai/codex` at
`e72da2b53805894878023d01949a25a082e0a5cb`. C-Squad already invokes the native
`codex queue --thread ... --message ...` command for registered sessions.
That command uses `thread/queue/add` with text user input; the queue dispatcher
starts it when the target thread is idle. Acceptance does not prove consumption.
The newer native `send_message` and message board require identities registered
within Codex's agent tree. They are not interchangeable external inboxes for
C-Squad's independently started root sessions and account environments.

See the pinned official
[queue implementation](https://github.com/openai/codex/blob/e72da2b53805894878023d01949a25a082e0a5cb/codex-rs/tui/src/session_queue_commands.rs),
[idle dispatcher](https://github.com/openai/codex/blob/e72da2b53805894878023d01949a25a082e0a5cb/codex-rs/ext/queue/src/service.rs),
and [message board contract](https://github.com/openai/codex/blob/e72da2b53805894878023d01949a25a082e0a5cb/codex-rs/agent-message-board-client/README.md).
The installed CLI was 0.157.0; it was not assumed identical to that source SHA.

## Batching boundary

Before queue transport, already-pending routine automatic reports for the same
Codex recipient can share one input. Current ledger filtering runs again at
claim time. The recipient lock, generation and delivery attempt still fence
transport and recording. Each constituent retains its own ID, sender, task and
success/failure record. The batch is bounded at eight messages and 24 KiB; an
individually larger message continues through the existing single-message path.

No waiting window is introduced to fill a batch. Freeform messages and urgent
questions, decisions, failures and recovery remain independent. An unfinished
message of those kinds is an ordering boundary. Another recipient's messages,
startup input, records claimed by another delivery and retry backoff are not
absorbed. Claude's native peer socket path is unchanged.
An older unfinished delivery to the same recipient also holds later routine
notices, including when the earlier record is in retry backoff or claimed by
another sender. Waiting does not consume a transport attempt. Urgent/freeform
messages keep their existing immediate individual path.

This does not retract notices already accepted into Codex's native queue, ensure
exactly-once delivery after a transport crash, or make queued input interrupt a
running turn. Those require a separate native adapter design, not more forceful
prompt injection. CLI table summaries likewise retain explicit full JSON for
complete scope, evidence history and automation.
