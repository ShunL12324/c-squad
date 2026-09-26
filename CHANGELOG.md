# Changelog

Release dates use UTC. This file starts with the verified v0.7.0 and v0.7.1
release history; earlier releases remain available on
[GitHub Releases](https://github.com/ShunL12324/c-squad/releases).

## v0.12.9 — 2026-09-26

### Changed

- Polish the original member and task panels with restrained terminal icons,
  stronger titles, muted metadata, and three-row padded Details and Back actions.
  Short member panes retain task IDs while keeping Master visible.
- Batch already-pending routine Codex notifications for one recipient before
  native queue delivery, preserving each message identity and delivery outcome.
  Keep urgent, freeform and startup messages independent; skip stale notices
  and preserve ordering behind earlier unfinished recipient deliveries.
- Reduce duplicate report fields and repeated agent handoffs. Prefer consolidated
  readiness and terminal monitoring outcomes over intermediate confirmations.
- Make `task inspect --output table` a concise status summary with current
  evidence, failures, missing review/test results and a full-JSON route.
  Preserve the existing default JSON format for automation.

### Fixed

- Keep nested member metadata on its card background and align padded button
  click regions with visible rows after scrolling. Correct short-pane sizing.
- Exclude audit-only legacy Brief records from notification batching and ordering
  so they cannot block current notices or be replayed.

## v0.12.8 — 2026-09-26

### Changed

- Restore the original member and task panels from v0.12.1, including member
  colors, card layout, scrolling and mouse/keyboard interaction. Remove the
  subsequent Bubbles list and tview side-panel migrations and their prototypes.
- Keep Active/Done task filters, cancelled task status, and the full-width
  View details action. Retain the fixed three-row workspace header and later
  startup delivery, notification and member-switch performance fixes.

## v0.12.7 — 2026-09-26

### Changed

- Refine member and task panels with borderless card surfaces and consistent
  padding. Restore each teammate's persistent color on names, task chips and
  owner labels; keep current session and keyboard focus distinct without outlines.
- Place member status and task chips together, mute the no-task placeholder,
  and remove unused metadata rows. Compact task cards fit three complete items
  at 40 by 46 terminal cells, with bounded summaries and full content in details.
- Keep native tview buttons visibly filled and align their inset backgrounds
  with the surrounding cards. Hide page numbers when there is only one page.

## v0.12.6 — 2026-09-25

### Changed

- Use tview controls for the member and task side panels, including real
  buttons for task tabs, details, back and close actions. Keep full-width
  detail actions with padding and Active/Done tabs with cancelled tasks in Done.
- Display members on filled card surfaces with colored state and task chips.
  Distinguish the current session from keyboard focus without a round dot;
  adapt cards and actions to short panes while keeping Master pinned.

### Fixed

- Keep the workspace header at three rows during divider drags, member
  switches and layout repair. Preserve sidebar width dragging and defer
  header repair while the terminal layout is reflowing.
- Keep keyboard focus and member activation aligned, scope mouse clicks to
  the intended card, and escape external text in styled panel content.

## v0.12.5 — 2026-09-25

### Changed

- Refine the member roster with standard Bubbles list styling and compact
  six-row items. Keep status, task, Git branch and directory metadata readable
  at narrow widths, with distinct current-session and keyboard-focus markers.

### Fixed

- Keep the focused member visibly selected across independent mouse-wheel
  pages. Align native mouse checks with rendered member titles instead of
  fixed row offsets.

## v0.12.4 — 2026-09-25

### Changed

- Use standard Bubbles list, delegate, paginator and viewport components for
  member navigation and task scrolling. Preserve the pinned Master, a separate
  current-session marker, mouse actions and narrow-terminal layouts.
- Run Linux CI and release checks with pinned tmux 3.6 and libutempter enabled,
  covering the upstream fix for missed pane-exit notifications. Document the
  compatibility limit of older tmux builds that use libutempter.

### Fixed

- Keep the first Codex startup message pending until its matching native
  submission hook arrives. Confirm the visible draft before Enter, retry only
  that draft without duplicate pasting, and refuse input into attached or busy
  sessions. Later messages wait behind the startup submission. This does not
  resolve external Codex account-routing timeouts; an unidentifiable draft stays
  pending instead of sending keys blindly.
- Disable the Master's crash hook before intentionally stopping its process,
  preventing redundant asynchronous shutdown callbacks during team cleanup.
- Synchronize native divider-drag checks with both drag start and release;
  adapt panel assertions to the standard list renderer.

## v0.12.1 — 2026-09-25

### Changed

- Simplify the task panel to Active and Done tabs. Cancelled tasks appear under
  Done with their own status badge. Make View Details fill the card width with
  padded text, remove duplicate task-footer shortcuts, and remove the unused
  `g` shortcut to Master.
- Keep independent developers working in parallel, but have Master schedule
  testing after all related modules of a feature are complete and integrated.
  Developers report implementation readiness; reviewers focus on review;
  testing is an explicit assignment to a developer or tester. Startup and
  recovery instructions discourage tests after every small edit and repeated
  runs by different members. Related tasks can share one integrated candidate
  and its exact-commit test results, with approval and merge performed one task
  at a time. Required review and test evidence remain in place.
- Clarify that owners may integrate assigned branches inside their own task
  worktrees; only Master approves and merges tasks into the target branch.
  Prefer completion notifications over long fixed sleeps and ignore obsolete
  task notices without sending another acknowledgment.
- `board` and `member list` read stored runtime observations instead of waiting
  for synchronous probes. Results flag observations older than 30 seconds.
  Claude session listings are shared within one observation pass when command,
  environment and working-directory requirements are compatible.

### Fixed

- Avoid duplicate tmux client and pane-geometry queries during member switching,
  reusing the validated source session and refreshing destination geometry only
  when resizing or repairing its layout.
- Do not generate self-assignment notices. Pending automatic availability,
  assignment and CC notices are checked against the current task before
  delivery, including owner handoffs and completed tasks. Existing unfinished
  notices are migrated conservatively using their generated formats; ordinary
  member messages are preserved.
- Keep routine automatic reports for a working Codex recipient in C-Squad's
  outbox so obsolete reports can expire before entering the native queue.
  Direct messages, questions and failure reports are not held. A stale activity
  timestamp allows delivery after five minutes without a hook update; messages
  already handed to Codex cannot be withdrawn.
- Separate structured observation output from shell diagnostics. Shell-mode
  Claude helpers can emit startup lines before a complete JSON array; direct
  invocations and trailing non-JSON output remain strictly validated.

## v0.12.0 — 2026-09-24

### Added

- New installations write six editable launch profiles: Claude junior, senior
  and expert use `sonnet[1m]`, `opus[1m]` and `fable[1m]`; Codex junior, senior
  and expert use `gpt-6-luna`, `gpt-6-sol` and `gpt-6-astra`. Both default
  pointers select `claude-senior`. No personal account paths are included;
  existing configurations and legacy launch defaults are preserved.
- `csquad update` upgrades csquad through the channel that installed it: npm
  (any global prefix, including nvm), Homebrew (any prefix), the APT source,
  or a local `.deb`, which it downloads from the latest release and verifies
  against the release checksum and its package fields before `sudo apt-get
  install`. Archives and other installs get the command to run instead.
  `update` shows the commands and asks first, runs `sudo` only in an
  interactive terminal, refuses member sessions, and `--check` installs
  nothing. `--check` and the `.deb` path query GitHub and need it reachable
  within its unauthenticated rate limit; the release checksum file is not
  signed, so the `.deb` path is weaker than the signed APT source.
- Installing a new csquad no longer changes running teams. Each team runs a
  private copy of the build it started with, stored under
  `~/.local/share/csquad/versions/` (or `CSQUAD_VERSIONS_DIR`), and a newer
  csquad hands team commands to that copy. `resume` moves a team to the
  installed version; it refuses an older release and asks when two builds
  cannot be ordered. Writes from any other build are refused. `csquad repin
  TEAM` recovers a team whose copy is missing or damaged. Old copies are not
  deleted automatically; `update --check` shows the space they use. `doctor`
  warns when more than one csquad is on PATH.

  **First upgrade:** teams started by 0.11 or earlier have no private copy
  yet, so an upgrade reaches them at once. Stop them, upgrade, then resume
  them; from then on upgrades leave running teams alone. Never run an older
  csquad against a team a newer one has pinned: it drops records it does not
  know. On macOS a build is identified by hashing the file it started from,
  and macOS has not been tested on real hardware.
- `task assign TASK --owner NAME --cc A,B` copies members on an assignment
  with one no-reply notice each. They do not become participants. An unknown
  or removed name fails the whole command, and repeating it sends nothing new.
  A name repeated in `--to` now gets one assignment notice instead of two.
- `task cancel TASK --reason TEXT` lets master stop a task. The reason, actor
  and time are recorded, and the task becomes `cancelled`, a final phase that is
  not success: it satisfies no dependency, shows no completion mark and accepts
  no further operation. Its owner is free for other work, and its workspace,
  candidate and evidence are kept. Pending notices about the task are
  withdrawn, its open questions are closed, and its members are told to stop.
  A task that is preparing its workspace or merging cannot be cancelled until
  that finishes or is aborted. A runtime left running by an older version is
  replaced after the upgrade. Do not downgrade once a task has been cancelled:
  an older csquad treats the task as unfinished, keeps its owner occupied and
  its dependents unclaimable, may send its members resume-work notices, and
  drops the cancellation record when it rewrites the ledger. Master's
  instructions list the command and say that a cancelled task satisfies no
  dependency; running sessions receive them at their next eligible hook.
- Master is told once when an in-progress task seems stalled: every member on
  it has been observed quiet (a finished, failed, crashed or stopped turn) for
  5 minutes, with no blocker, open question, pending gate or deliverable
  message. The notice changes nothing else and is superseded if the task moves
  on before delivery. A Codex member interrupted by the user is now shown as
  `interrupted` until its next prompt. Background work the engines do not
  report can cause a notice, and a Claude member stopped with Esc may look
  busy and cause none. The window is fixed at 5 minutes. This does not use the
  engines' native goals, and it never continues or completes a task.

### Changed

- The task panel's second tab is **Done/Cancelled** (**Closed** when narrow):
  it holds finished and cancelled tasks with distinct badges, while every
  unfinished phase stays under **Active**. A member card no longer lists a
  cancelled task as its current work.

### Removed

- **Breaking.** The task panel's **Brief report** button, its `b` and `r` keys
  and `task brief TASK` are removed; ask master directly. Submission and
  evidence reports, task progress and `task submit` summaries are unchanged. A
  Brief request recorded by an older version still loads and is never
  delivered or retried.

## v0.11.1 — 2026-09-23

### Added

- `member restart NAME --reprofile` and `member replace NAME --reprofile`
  re-read a member's engine, model and environment from its profile in the
  current configuration, and `--profile NAME` moves it to another profile;
  `recover --reprofile` does the same for Master. The member keeps its name,
  tasks and directory, and its conversation resumes unless the engine or
  account directory changed. The change is printed with variable names only,
  before the member stops. `CODEX_HOME` and `CLAUDE_CONFIG_DIR` keep the values
  recorded when the member was added unless the profile sets them, so running
  the command from another terminal does not move the member to that
  terminal's account. A configuration that does not load stops the command
  before the member is touched. Without the flag, restart keeps the member's
  settings, and resume keeps engine and model, updates only variables
  inherited from the profile, and names the members whose profile still
  differs.

### Changed

- Agent instructions are shorter and split by role. Each communication rule is
  stated once, workers see only the commands of their work loop while Master
  also sees task and member administration, and a member with a known task is
  told to run `task inspect` before reading the whole board. Codex startup
  instructions shrank from 1,272 to 741 words for a worker and from 1,648 to
  1,088 for Master. Running members receive the new text at their next
  eligible hook.
- A task assignment or availability notice tells the member to run
  `task inspect TASK`, which shows that task's workspace, acceptance and
  milestones, instead of reading the whole team board.
- `--help` and completion recommend one spelling per operation: `new`,
  `question` and `message reply`. `start`, the `help request|list|answer`
  alias, top-level `reply`, and the `--team`, `--state-dir`, `--member` and
  `--generation` selectors are no longer listed, and every one of them still
  parses and behaves as before. `--team-name` remains the listed selector.
- The task panel marks a task Completed from the agent workflow alone: a code
  task once master merges the reviewed and tested candidate, a task without a
  workspace once master approves it. Unfinished, blocked and externally closed
  tasks are never marked. An old ledger's user confirmation still loads and is
  shown in the details as a legacy record.

### Removed

- **Breaking.** `task confirm`, the task panel's **Confirm completion** button
  and its `c` key are removed; completion now follows the agent workflow as
  described above. A script that ran `task confirm` must drop that step.

### Fixed

- The npm cold-Zsh test uses private completion initialization functions so
  insecure completion directories on a CI host cannot interrupt the test.
  It still executes the printed loading instruction unchanged and keeps
  Zsh's permission checks enabled; installed shell behavior is unchanged.
- Members start even when a rendered Codex prompt or a long `PATH` (as on WSL)
  exceeds tmux's 16 KB command limit. The engine arguments and environment now
  reach the runner through a private file instead of the tmux command line, so
  `start` and `resume` no longer fail with `command too long`.
- A message delivered while its sender restarted is recorded as sent instead of
  staying in `sending` and being delivered again after recovery.
- Messages to a removed member are closed instead of being retried by the
  runtime on every pass and delivered as a backlog to a later replacement.
  `message send` and `message reply` refuse a removed member, and answering its
  question records the answer without queueing a message.
- Restarting, replacing or removing the member you are watching moves your
  terminal to Master instead of detaching it from tmux.
- An argument ending in `;` or `\;`, including a message that is only `;`,
  reaches tmux unchanged instead of losing a character or splitting the
  command. A project path containing `#` characters such as `#S` no longer
  breaks shutdown and navigation, so Master's exit closes the team again.
- An empty value in a profile's `env` unsets the variable, as documented, for
  every variable. Only `CLAUDE_CONFIG_DIR` used to be removed; `CODEX_HOME = ""`
  reached Codex as an empty string.
- A profile without `engine` fails when the configuration loads, not at
  `member add` or `start`. A legacy template that set no engine migrates with
  the engine its role used to default to, so an old configuration still loads.
- A member whose profile was edited to another engine launches its own engine by
  name with a warning, as for a deleted profile, and a resume keeps its
  environment. It used to run the other engine's command with its own arguments.
- A legacy configuration with `master_model = ""` and no `master_engine` keeps
  Master on Claude's native default model instead of moving it to `opus[1m]`.
- A legacy `engine`, `model`, `master_engine` or `master_model` set next to the
  matching `[templates.developer]` or `[templates.master]` overrides only the
  fields it writes. The migrated profile keeps the template's `env`, such as
  the account directory, and its other settings, instead of starting from an
  empty profile.
- `resume --engine` and `resume --model` name the profile field that replaces
  them instead of failing with an unknown-flag error.
- Scrolling a task's details stops at the last page, so scrolling back moves
  the view at once.
- Table output aligns columns by display width, and `member list` cuts
  instructions at 48 columns rather than 48 characters, so CJK text and emoji
  no longer push the DIRECTORY column out of line.
- Release notes are published from the version's changelog section with the
  source commit, instead of an empty body, and a missing or undated section
  stops the release before any draft exists.
- The GitHub Release stays a draft until the npm tests pass on both platforms.
  Homebrew can still only be tested after publication.
- Rerunning the release of an older tag no longer rolls APT and Homebrew back;
  release runs are serialized and the Formula push is fast-forward only.
- The release workflow's Linux npm tests install Zsh and check both shells'
  completions, and the packaging timeout tests wait for child startup instead
  of a fixed deadline. GitHub Actions moved to their Node 24 major versions.

### Not reproduced

- #26, `member restart --cwd` losing a Claude conversation: with Claude Code
  2.1.280, `--resume` finds a session Claude created from another directory
  and from another worktree, so no change was made.

## v0.10.0 — 2026-09-23

### Added

- `member profiles` lists the launch profiles `member add` can use, with each
  profile's engine, model, custom-command flag and selecting pointer. It names
  environment variables without their values. Master's command reference now
  points to it before `member add --profile`.

### Changed

- A running team reads its profiles from the current configuration at every
  `member add` and member launch. A profile added while the team runs is usable
  at once, and an edited launcher reaches a member's next restart as well as a
  resume. A configuration that fails to load keeps the saved profiles with a
  warning. A member's engine, model and environment are still fixed when it is
  added.

## v0.9.1 — 2026-09-23

### Fixed

- Closing Tasks from the tasks view keeps the member sidebar. It used to hide
  both panels, including member navigation.
- Upgrading while a team is running no longer strips profiles from its ledger.
  The runtime protocol now changes with the ledger schema, so the first command
  after an upgrade replaces a runtime left by v0.8.
- A legacy `config.json` is rewritten as JSON after migration. It used to be
  rewritten as TOML, after which every load failed.
- A legacy configuration that sets only `model` or `master_model` keeps the
  engine that role used to default to, instead of migrating to a profile with
  no engine that the load then rejected.
- A Claude worker launched with an opening prompt and no model receives that
  prompt. The tool denial list used to consume it as a tool name.
- A member added with the built-in default is no longer bound later to
  whichever profile launches the same engine. Only members saved before
  profiles existed are backfilled.
- `team remove` refuses a workspace with assume-unchanged or skip-worktree
  entries, whose edits `git status` cannot see, as workspace cleanup already
  did.
- A removed launch flag is explained before any other check, whatever its
  value. `--engine gpt`, `member add` outside a team and an empty `--model`
  used to report something else or pass silently.
- Migration reports engine or model settings it ignores because a profile
  pointer is already set, typically in a project `.csquad.toml` written before
  profiles.
- When a `.before-profiles` backup already exists, the configuration is
  reported as migrated in memory only, not as rewritten. A symlinked
  configuration keeps its link and the migration is written to its target.

## v0.9.0 — 2026-09-23

### Removed

- **Breaking.** The top-level `engine`, `model`, `master_engine` and
  `master_model` settings, the `[env]` and `[startup_env]` tables, and the
  `[engine_commands.*]` tables are gone. A launch profile now holds all of it.
- **Breaking.** `member add` no longer accepts `--engine`, `--model`, `--env`,
  `--role` or `--template`, and `start` no longer accepts `--engine`,
  `--model` or `--env`, nor does `resume`. Each removed flag reports the profile
  field that replaces it. A member's identity comes from `--instructions`; its
  name is its label. `doctor --engine` is a diagnostic and is unchanged.
- **Breaking.** The built-in Master model is now `opus[1m]` rather than `opus`.
  A configuration that never set `master_model` starts Master on a different
  model after upgrading. Set `model` in the profile that `master_profile`
  selects to pin the previous value.
- The `needs_attention` delivery state, `Message.DeliveryNote` and the legacy
  ACK-warning migration that `normalizeState` ran over every message on every
  read and update.
- `Message.RecipientGeneration`. Production code never read it, and the prompt
  rules that asked members to compare generations themselves could never fire,
  because delivery already guarantees the stamped generation is the recipient's
  current one. The `recipient_generation` header stays as audit information.

### Added

- Launch profiles. A `[profiles.NAME]` table holds `engine`, `model`, `env` and
  an optional `command`, and `default_profile` / `master_profile` select the
  profile used when `--profile` is omitted. A generated configuration ships both
  defaults as real, editable tables rather than hidden constants. Without the
  pointers, members start `codex` and Master starts `claude` with `opus[1m]`.
  Profile settings are validated when the configuration loads.
- A profile never holds responsibilities, and its name carries no meaning: a
  profile called `master` applies to Master only when `master_profile` or
  `start --profile` selects it, and a member's own text is never matched against
  profile names.
- `member list` summarises each member's instructions in place of the removed
  role column, and `member inspect` surfaces the full text.

### Changed

- Existing configurations migrate on load and are rewritten once, after the
  original is saved as `config.toml.before-profiles`. Top-level engine settings,
  `[env]`, `[startup_env]`, `[engine_commands.*]` and legacy `[templates.*]`
  become equivalent profiles. Within each profile the merge order stays what it
  was: `[env]`, then the profile's own entries, then `[startup_env]` on top.
  Legacy template prompts are dropped with a warning. Comments in the original
  file do not survive the rewrite; the backup keeps them.
- Members that predate profiles inherit the profile their engine migrated into,
  so a launcher configured through `engine_commands` keeps working after the
  upgrade instead of falling back to the bare engine name on the PATH.
- A member's environment now has two layers: the inherited environment, then its
  profile's `env`.
- Engine, model and environment are still materialised when a member is added,
  but the launch command is resolved through the member's recorded profile, so
  an edited wrapper reaches the next restart. A profile deleted after the member
  was added launches the engine by name with a warning instead of failing, and
  that member keeps the account it was added with.
- Startup checks probe the command the member will actually launch, including a
  profile's wrapper, rather than the bare engine name.
- The ledger version rises to 3. Delivery records left in `needs_attention` by
  an older release are normalized once, off the hot path. Without this they
  loaded but matched no branch of `deliver`, staying undelivered and uncleared
  forever.

### Fixed

- Workers can no longer enter plan mode. Claude Code exposes `EnterPlanMode` and
  `ExitPlanMode` as separate tools, so denying only the entry name left plan mode
  reachable and the rule resting on the prompt alone.
- Repeated team recovery no longer accumulates identical wake-ups. Each task's
  recovery notification carries a stable request key and is reused while the
  recipient has not received it; a delivered notification is never reused, so a
  later genuine interruption still wakes its owner. A task finished in the
  meantime supersedes its own pending notification instead of waking anyone.
  Ordinary messages queued before a restart are still delivered. (#14)

## v0.8.0 — 2026-09-21

### Added

- Configure Claude Code and Codex launchers independently with
  `engine_commands.<engine>.executable` and literal fixed `args`. For example,
  `cfuse --cc` can receive the existing Claude Code arguments.
- Opt into Bash or Zsh for persistent aliases, including aliases with leading
  environment assignments. Startup checks, doctor, engine launches, and helper
  commands use the configured launcher. Arguments retain spaces, quotes, and
  shell metacharacters literally.

### Changed

- Team resume reloads engine command defaults from current configuration;
  member restart and recovery retain the saved command configuration.
- Preserve explicit environment overrides after shell initialization, then
  apply alias-local assignments. Without an explicit PATH override, retain
  directories added by the shell rc file and put the member's bound C-Squad
  launcher first.
- Clarify layout, confirmation, and acknowledgment comments; remove redundant
  conditions and helpers; separate task operations and panel UI responsibilities.
- Brief sends Master a short English native user prompt asking for task progress,
  remaining work, and blockers, with a direct reply in the user's language.
  It no longer creates team messages, ACK/reply chains, or outbox retries.
  Each deliberate click is a new question; older Brief records remain audit-only.

### Fixed

- npm installations now show a one-time interactive completion setup hint. Run
  `csquad completion install --shell zsh`, then execute its printed loading line
  in the current Zsh and add the same line at the end of `~/.zshrc`, after any
  completion framework. Direct registration handles missing `fpath` entries and
  old compinit caches without deleting caches or editing startup files for users.
- npm automation, completion protocol calls and agent panes remain quiet. The
  setup hint stays suppressed across upgrades and Node-prefix switches; installed
  completion files remain in user-owned storage after npm uninstall. Installation
  alone does not activate completion in an already-open shell.

## [v0.7.1](https://github.com/ShunL12324/c-squad/releases/tag/v0.7.1) — 2026-09-21

- Make routine message acknowledgments optional while retaining explicit ACK,
  reply, retry, and Brief commands.
- Preserve successfully sent messages across restart and recovery without
  automatic redelivery or missing-ACK warnings.
- Skip sent messages during sync and conservatively migrate known legacy
  acknowledgment warnings.
- Update shared English prompts to keep routine progress in the ledger and
  follow up when the context requires it.

Release source: `6d88be085ccd64efd037417221a93ea5b19d71a2`.

## [v0.7.0](https://github.com/ShunL12324/c-squad/releases/tag/v0.7.0) — 2026-09-21

- Add tmux-style `new -s NAME` and clarify team lifecycle commands.
- Preserve member panel geometry across navigation and resize, including task
  overlays.
- Record independent user confirmation separately from technical task completion.
- Aggregate task reports and suppress duplicate or stale notifications.
- Preserve explicit member environment overrides and validate resumed launch
  context.
- Use shared English runtime prompt templates.
- Add explicit task workspace cleanup with preview and safety checks, without
  background forced cleanup.

Release source: `97b45401595da61486e1c63ac68218f48a2f0feb`.
