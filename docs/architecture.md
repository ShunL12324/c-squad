# Architecture and reliability contracts

Package boundaries follow responsibilities and dependencies. Files in a Go
package share unexported implementations; splitting a file improves navigation
but does not create an access boundary.

```text
cmd/csquad/         Entry point and exit handling
internal/
  cli/             Cobra commands, arguments, help, completion, and exit codes
  preflight/       Runtime dependency checks
  buildinfo/       Build version metadata
  squad/           Tasks, messages, lifecycle, engine adapters, and ledger transactions
  config/          TOML and legacy JSON loading, migration, defaults, and overlays
  agentenv/        Environment validation, overrides, and native unset semantics
  process/         Helper commands, process snapshots, identity checks, and tree cleanup
  filelock/        Unix interprocess locking
  tmux/            Socket-scoped commands and exact session lookup
  teamui/          Bubble Tea panels, task cards, viewport and pointer handling
```

Dependencies flow from `cmd` through `cli` and `squad` to supporting packages.
Additional dependencies are `config -> agentenv` and `tmux -> process -> agentenv`.
Supporting packages neither import `squad` nor read its task, member, or team
ledger. Concrete types and functions represent capabilities with a single
implementation; package separation alone does not require service or repository
interfaces.

## Runtime contracts

- Configuration loading handles overlays and compatibility migrations. Running
  teams use their startup snapshot. The orchestration layer decides when to load
  configuration and how to display it.
- The tmux client resolves servers and exact targets. Team ownership checks,
  navigation, and authorization to remove sessions belong to orchestration.
- `process.Command` manages cancellable helper-command process groups. Its
  timeout does not limit interactive agent tasks.
- Process identity combines PID and start time. Cleanup checks both before
  termination to reduce the risk of acting on a reused PID. A snapshot cannot
  eliminate all operating-system races.
- Tree cleanup temporarily suspends processes to reduce the opportunity for
  children to escape. On failure, it revalidates identity and resumes surviving
  suspended processes. If a recovery snapshot also fails, it returns joined
  errors so orchestration can retain the pending-cleanup state.
- File locks provide mutual exclusion. Lifecycle operations acquire locks in
  `team-lifecycle -> member` order to coordinate process replacement and writes
  from previous generations.
- SQLite is the durable source of truth for tasks and messages. Writing a
  message to the outbox does not mean it was delivered or acknowledged.

## CLI, state, and errors

The Cobra command catalog defines help, flags, required arguments, and
completion. It passes parsed commands to `squad.Execute` without constructing
shell commands. Internal native-engine arguments are passed as separate argv
entries; arguments after `--` are not interpreted by C Squad.

Task, message, member, team, checkpoint, and question states use distinct string
types and constants, as do dispatch modes, evidence kinds, and engines. String
encoding preserves existing JSON/TOML compatibility. Go constants are not closed
enums, so inputs still require validation. Native waiting reasons can extend the
`waiting_` family. Model names, user roles, message bodies, and event descriptions
remain open-ended values.

Repeated business failures that callers must distinguish use sentinel errors:
stopped teams, stale generations, missing resources, Master-only operations, and
member limits. Operations wrap context with `%w` and preserve multiple failures
with `errors.Join`. Error text is not a protocol. The CLI handles exit codes and
recovery hints centrally; operation-specific error messages stay near the code
that produces them.

`preflight` checks executable availability, not authentication, quota, or every
engine-version combination. Installation checks complement runtime checks.
Research tasks can run without Git; code tasks must check Git availability.
Checks do not install software or launch engines during help or completion.

## Comments and remaining boundaries

Exported API comments describe inputs, outputs, side effects, and caller
responsibilities. Complex paths document invariants, lock order, recovery, and
platform limits. Comments on native-engine behavior identify the relevant
context instead of implying a universal protocol guarantee. The revive linter
checks form; review checks accuracy.

`squad` remains a substantial application package: CLI output, business rules,
SQLite transactions, and native launch adapters are not fully separated. A
useful next boundary is structured use-case results, followed by presentation
and storage separation. Moving an exported Store type to another directory would
not, by itself, decouple those responsibilities.

Production readiness also requires native-engine compatibility acceptance,
Linux/macOS/WSL runtime validation, observable failures, and verified releases.
Package structure and comments do not replace those checks.

## Development and testing

`make fmt` formats code. `make check` runs formatting checks, golangci-lint, and
`go test -race ./...`. Development tools use pinned versions in the ignored
`.tools/` directory. `make snapshot` uses a pinned GoReleaser to build Linux and
macOS packages for amd64 and arm64, including completions and checksums, without
publishing. When Git metadata is absent, it stages a temporary source repository
instead of modifying the original directory.

Tests use standard Go `testing`. Keep `*_test.go` beside the code it exercises;
normal builds exclude these files. Table-driven tests cover arguments and state
transitions. Isolate files, environment, and processes with `t.TempDir`,
`t.Setenv`, and `t.Cleanup`. Tests that change environment variables must not use
`t.Parallel`. Assert behavior, state, exit codes, and `errors.Is` rather than
complete error messages.

Integration tests use real Git, tmux, and CLI subprocesses with fake native
engines, so they do not consume model quota. Tests that need tmux skip when it is
absent; full validation and release CI must install it. Real Claude Code/Codex
acceptance is separate: check startup, message round trips, task delivery, and
cleanup/recovery after Master failure. Passing automated tests does not establish
native-engine compatibility. macOS and WSL still need runtime acceptance;
cross-compilation only establishes buildability.

## Terminal presentation

Tmux owns native engine terminals and session lifetimes. Bubble Tea handles
panel input and refreshes; Lip Gloss renders their styles. The UI reads snapshots
from the ledger and asks orchestration to navigate. It never owns engine input
or infers task completion from terminal output.

The member sidebar, native terminal, and task board occupy separate panes.
Task cards share rendered bounds with hit testing, so wrapped titles cannot
shift click targets. Wide terminals show both panels by default; compact layouts
and popups preserve room for the native terminal. The status bar contains team
identity and shortcuts rather than a second member list.

A future terminal backend must cover pane creation, client-specific navigation,
input delivery, screen capture, lifecycle events, and recovery before replacing
tmux. Native Windows also requires replacing Unix process-group and file-lock
operations; changing the rendering library alone does not provide that support.
