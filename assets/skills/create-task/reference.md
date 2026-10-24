# create-task reference

Read this only when a launch needs more than the core skill says.

## Engine arguments after `--`

- Claude (default): passed to `claude --bg` unchanged, e.g.
  `-- --model opus --effort high` or `-- --settings '{"model":"sonnet"}'`.
  They override csquad's defaults (the model, and
  `--permission-mode bypassPermissions`). `--bg`, `--name`, `--print`,
  `--resume`, `--continue`,
  `--fork-session` and `--append-system-prompt` are set by csquad and are
  refused.
- Codex: thread options named as the `codex` command names them:
  `-m/--model M`, `--effort low|medium|high|xhigh`,
  `-s/--sandbox read-only|workspace-write|danger-full-access`,
  `-a/--ask-for-approval untrusted|on-request|never` and
  `-c/--config KEY=VALUE` (repeatable). Defaults: `danger-full-access` and
  `never`. Anything else is refused before the task starts.

A model alias must be given explicitly: without `--model` the engine's
default model runs, whatever the routing meant.

## Flags

- `--after T3,T4`: existing tasks only. The task is queued
  (`queued T5 · ...`) and starts once all of them are done (merged, or
  finished with finish-task); its prompt then gets what they left (merged
  branch, final message). A task that is only ready does not release it.
- `--check codex|claude[:MODEL]|cmd:COMMAND`, `--max N` (default 3): each
  time the worker stops, a fresh read-only reviewer (or the command, in the
  task's worktree; exit 0 passes) checks the work. Findings go back to the
  worker until the check passes or the rounds run out.
- `--merge [BRANCH]`: when the work is done (and checked), the worker
  rebases its branch onto BRANCH (default: the branch of the main checkout)
  and csquad fast-forwards it and cleans up. Not with `--isolation none`.
- `--isolation none`: the task edits its working directory in place (to see
  its changes live, e.g. through a running dev server), makes no branches and
  leaves its changes uncommitted. One in-place task per checkout at a time.
- `--fork`: the task starts as a fork of this session and inherits the
  conversation so far, so its prompt can say "the plan we agreed on". It runs
  in this session's engine (pass `--engine codex` from a Codex thread). Its
  first turn re-reads the whole inherited context, so prefer a
  self-contained prompt when the conversation is long. Not with `--after`.
- `--no-worker-rules`: leaves out the worker rules, for a prompt that brings
  its own.

A task that stops for the user shows `needs you · reason`;
`{{CSQUAD}} retry T12` resumes it where it stopped once the user has looked.

The user manages how many tasks run at once. If many are already working
(`{{CSQUAD}} status --all`; use it only to count, never to report other
sessions' tasks), mention it rather than holding tasks back yourself.

## When a launch fails

- A Claude task in an untrusted workspace: ask the user to open `claude` in
  that directory once and accept the trust prompt.
- A Codex task that cannot reach the app-server: csquad starts it
  (`codex app-server daemon start`); if that fails too, show the user the
  error.
- Anything else: show the user csquad's message; it names the flag or path
  and the fix.
