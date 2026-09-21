# Changelog

Release dates use UTC. This file starts with the verified v0.7.0 and v0.7.1
release history; earlier releases remain available on
[GitHub Releases](https://github.com/ShunL12324/c-squad/releases).

## v0.8.0 — Unreleased

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

### Pending integration

- Brief is being changed to send Master a short English native user prompt
  asking for task progress, remaining work, and blockers, with a reply in the
  user's language. This must be verified after integration before release:
  Brief should no longer create a team message, ACK/reply chain, or outbox retry.

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
