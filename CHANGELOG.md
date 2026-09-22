# Changelog

Release dates use UTC. This file starts with the verified v0.7.0 and v0.7.1
release history; earlier releases remain available on
[GitHub Releases](https://github.com/ShunL12324/c-squad/releases).

## Unreleased

### Added

- Define reusable launch profiles in `[profiles.NAME]`: engine, model,
  environment overrides, and an optional command. Select one by name with
  `member add --profile` or `start --profile`, or point `default_profile` and
  `master_profile` at the profile each role uses by default. A new configuration
  is written with the built-in defaults as `[profiles.codex]` and
  `[profiles.claude-opus]`; without the pointers, members start `codex` and
  Master starts `claude` with `opus[1m]`. Profile settings are validated when the
  configuration loads.
- `member list` shows each member's responsibilities in place of the removed role
  column, reduced to their first line; `member inspect` surfaces the full text at
  the top level and prints it only once.

### Changed

- A member's launch is determined entirely by its profile. The command line
  carries only which profile to start and what the member is for, so a member
  always launches exactly what its profile says.
- A profile holds only how to launch. Responsibilities come from
  `member add --instructions` alone, and no configured text is looked up by a
  member's name to inject a prompt. Profile names carry no built-in meaning; a
  profile named `master` applies to Master only when `master_profile` or
  `start --profile` selects it.
- A member's environment now has two layers: the inherited environment, then its
  profile's `env`.
- Engine, model, and environment are still materialised when a member is added;
  the launch command is resolved through the member's recorded profile, so an
  edited wrapper reaches the next restart. A profile removed after the member was
  added launches the engine by name with a warning instead of failing, and that
  member keeps the account it was added with.
- Startup checks now probe the command the member will actually launch, including
  a profile's wrapper, rather than the bare engine name.

### Removed

- `--engine`, `--model`, `--env` and `--role` on `start`, `member add` and
  `resume`, and `member add --template`. Each one now fails with the profile
  field to set instead. `doctor --engine` is unchanged.
- The top-level `engine`, `model`, `master_engine` and `master_model` fields and
  the `[env]`, `[startup_env]`, `[engine_commands.ENGINE]` and `[templates]`
  tables. They are migrated to profiles the first time the configuration loads,
  and team snapshots created before this release are migrated on read, so an
  existing team keeps starting the engines it started before. Shared environment
  entries merge into every profile in the order the old launch path applied them
  — `[env]`, the profile's own values, then `[startup_env]` — and a legacy engine
  command merges into the profiles launching that engine, or into a profile
  created for it when none does. A saved team's existing members are pointed at
  the profile their launch settings became, so a configured wrapper or alias
  keeps launching them after the upgrade. The original
  file is copied to `config.toml.before-profiles` first; a failed write-back
  warns and keeps the in-memory migration. A template's `prompt` is discarded,
  with a warning naming `--instructions` as its replacement; the text remains in
  the backup. Migration carries written values over as they are; only a
  configuration that set no Master model picks up the new built-in `opus[1m]`.

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
