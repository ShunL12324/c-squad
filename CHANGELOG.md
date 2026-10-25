# Changelog

## 3.1.0 - 2026-10-25

### Added

- Native notifications. A task that needs you or is finished shows a
  notification titled with the task (`T2 · fix login redirect`): `needs
  input` (a question, a permission prompt, a held message, with the first
  line of the last message), `needs you` (with the reason), `ready` (the
  check passed), `done` (a task without a check or a merge ended its turn,
  with the first line of its final message), `failed`, and `merged` (a
  landing of `--merge`). Each change notifies once, from the first csquad
  process that sees it: the status line, a Stop hook, `wait` or any
  command. A task that works sends nothing. See
  `docs/design/notifications.md`.
  - Windows: a toast from the app ID `csquad`, which `install` registers for
    the user in `HKCU\Software\Classes\AppUserModelId\csquad` (no admin
    rights), with the icon in `%LOCALAPPDATA%\csquad\csquad.png`. It is
    shown through a hidden Windows PowerShell. WSL shows the same toast
    through `powershell.exe`. A native and a WSL installation share the app
    ID; each toast registers it again when the key or the icon is missing.
  - macOS: `install` writes `~/.csquad/csquad.app` (an agent app bundle
    with the icon and a copy of csquad), signs it ad hoc and registers it
    with LaunchServices. The bundle posts through UNUserNotificationCenter,
    called through purego, so the build stays without cgo. macOS asks once
    for permission. Without the bundle, csquad falls back to `osascript`
    (shown as Script Editor).
  - Linux: `org.freedesktop.Notifications.Notify` on the D-Bus session bus;
    nothing without a session bus.
- `csquad notify test` sends a test notification and prints the backend, or
  why there is none. `csquad notify on|input|off` keeps the setting in
  `~/.csquad/notify`; `CSQUAD_NOTIFY=on|input|off` wins over it. `input`
  sends only needs input and needs you.
- `install` prints the backend it set up (`Notifications: ...`);
  `uninstall` removes the app ID, the icon and `csquad.app`.
- `launch --no-merge`, `csquad hold T17` and `csquad release T17`: the user
  reviews T17 first. csquad never merges a held task for its dependents.
  When its work is complete its row reads `done · finish to start T18` (a
  green tick, the text bold, sorted after the urgent rows) and T18 reads
  `waiting T17 · review`; `status` shows the same with a `next:` line, and
  `status --json` has `hold` and `next`. Finishing T17 or `csquad release
  T17` starts T18. `hold` refuses a task launched with `--merge`.
- The skills: create-task tells the user that T17 is merged when it is done
  and adds `--no-merge` (or runs `hold`) only when the user wants to review
  it first; task-status and finish-task explain the held row.
- The database gains a `hold` column (migration 4). An older csquad refuses
  the upgraded database.

### Changed

- The database gets migration 3: the columns `notified`, `notified_at` and
  `notified_phase` record what was announced, with a compare-and-set, so
  concurrent processes never send the same notification twice. Tasks that
  are ready, need you, failed or are done (without a flow) at the update
  count as announced.
- The Stop hook of a task without a check or a merge now also starts a
  detached `csquad advance --stopped`, so its `done` comes without a status
  line (a Codex master session). `CSQUAD_NOTIFY=off` turns this off.
- The detached processes csquad starts also get `DBUS_SESSION_BUS_ADDRESS`,
  `WSL_DISTRO_NAME` and `WSL_INTEROP`.
- "T18 after T17" starts T18 by itself. A task that a waiting task lists in
  `--after` is merged when its work is complete (its worker stopped, or its
  check passed), as if it had `--merge`: into the branch of the main
  checkout, the target `finish` uses, through the same rebase and
  fast-forward. T18 then starts from the updated branch. Before, a task
  without `--merge` stayed `running` after its worker stopped (its row
  showed `✓ done`) and T18 stayed `waiting T17` until the user merged and
  finished T17, and nothing said so. A merge conflict stops T17 with
  `needs you · merge conflict` and T18 keeps waiting, as with `--merge`.
- It is decided each time csquad looks at the tasks, not at launch: a task
  queued after a task whose worker stopped long ago starts too, and chains
  queued with 3.0 move on after the upgrade (the status line spawns the
  merge). A task whose dependents were all cancelled is not merged.
- A task launched with `--isolation none` has nothing to merge: when tasks
  wait for it, it is done when its worker stops (its session is removed, as
  `finish` does), and they start.
- `launch` says how each dependency of a queued task gets done, for example
  `T17 merges into main when it is done, then T18 starts`. `status` shows
  the merge in the `flow:` line.
- `--merge BRANCH` keeps its target. A check's own process and `retry` that
  land a merge now start the tasks queued after it at once.

### Fixed

- A queued task started by a detached csquad (the advance the Stop hook or
  the status line spawns) could fail with `needs you · did not start:
  Workspace not trusted`. csquad set `CLAUDE_CONFIG_DIR` for its detached
  processes even when the user had not, and with it set Claude Code reads
  the workspace trust from `$CLAUDE_CONFIG_DIR/.claude.json` instead of
  `~/.claude.json`. Detached processes now get `CLAUDE_CONFIG_DIR` only
  when the user set it.
- csquad no longer hangs when Claude Code's background service stops
  answering. Seen with Claude Code 2.1.295: `claude agents --json` waited
  forever and ignored SIGTERM. `csquad status`, `peek` and `finish` then
  hung, and each status line refresh started another `claude agents` that
  never ended.
  - Every `claude` call has a deadline: 8 seconds for `claude agents` in a
    command, the status line's own budget there, 20 seconds for `claude
    stop` and `claude rm`, a minute for a launch. When the deadline passes,
    csquad kills the call with everything it started: SIGKILL to its own
    process group, or its process tree on Windows.
  - csquad records each `claude agents` call that runs in
    `~/.csquad/cache/claude-agents/`. When a call is still running a minute
    later because its csquad died, the next csquad kills it.
  - The status line never starts a call while another one runs. It takes
    that call's answer, or shows its last rows with a `?` after the state.
    After 2 minutes it shows one row, `claude agents not answering — csquad
    status for details`, instead of an empty ledger.
  - `csquad status` lists the tasks from its own store. A task's state reads
    `unknown: Claude Code's background service did not answer; last seen
    working 3m ago (unverified)`. One line at the top names the fix: `claude
    daemon stop --any --keep-workers`, then `claude agents`.
  - `finish` and `cancel` change nothing and say why, because they must know
    whether the session still works. `wait` says it once and goes on
    waiting. `peek` reads the transcript as before.
  - A request to the Codex app-server that gets no answer fails the same
    way: after 8 seconds for a read or a removal, after a minute for other
  requests.

## 3.0.3 - 2026-10-25

### Changed

- The status line ledger fills the terminal width first: it takes the fewest
  rows that fit, with as many columns as fit, up to one per task. Two tasks
  in a 280-column window now sit side by side in one row instead of two rows
  of one column with the rest of the window empty. For a number of rows it
  takes the fewest columns (3 tasks that fit in one row take 3 columns, not
  2 columns of 2 rows), and a column is still added only when every title
  keeps 16 cells. When four rows of tasks do not fit, it takes one column
  fewer and a `+N more` row, as before; 9 tasks at 160 columns now show in
  four rows of 3 columns instead of 6 tasks and `+3 more`. One row reads
  left to right, most urgent first; several rows fill column by column, as
  before, so the first column holds the most urgent tasks. The rule "up to
  four tasks in one column, ⌈n/4⌉ columns for more" of 3.0.1 is gone.

## 3.0.2 - 2026-10-25

### Fixed

- A skills directory that Claude Code and Codex share (`~/.claude/skills`
  linked to `~/.agents/skills`, a junction on Windows, the other way round, or
  both linked to a third directory) no longer gets the Codex skills written
  over the Claude ones. Before, Claude Code read the Codex instructions (for
  example, "run as a long-running command" instead of `run_in_background`),
  and every install or update reported each skill file as edited and wrote
  `.bak` files. Now install writes one copy of each skill there, with both
  agents' instructions as labelled paragraphs (**Claude Code:** ... /
  **Codex:** ...), says so (`Codex: ... is the same directory as ...`), and
  records the copy once in the manifest (`sharedSkills`). Update and
  uninstall handle the change to a shared directory, back to separate ones
  (the link removed), and a manifest from 3.0.1 that records both copies of
  the same files. A file that holds exactly what csquad wrote before, for
  either agent, is not reported as edited. Uninstall removes each file once
  and keeps the link, the shared directory and other skills in it.
- On Windows, an emptied skills directory that is a junction is no longer
  removed: Go reports a junction as irregular, not as a symlink.
- message-task and watch-task: the Codex sandbox note and the way to run
  `wait` are whole sentences of their own (the same instructions as before).

## 3.0.1 - 2026-10-25

### Changed

- `csquad status` indents a task's latest message, so its own `## ` headings do not
  read as the next task's header.
- A Claude task accepts cross-session messages (`"crossSessionInbound":
  "accept"` in its settings, unless `--settings` sets a value). A task runs in
  bypass mode, where Claude Code otherwise held every `csquad send` message
  and every fix round's findings until the user approved them in agent view.
- The status line's usage cell shows numbers instead of a trail: working,
  checking, fixing and merging rows show the fresh total and the change
  since the last refresh (`326k +4.1k`; `+0` faint; `+?` when the provider
  reports no or partial usage); a stall shows `idle 3m` in amber after the
  total (not for a checking task, which waits for its check). Other rows
  show their total, faint, as before. In a narrow cell the change goes
  first, then the total. The status line keeps two readings per task
  instead of nine. `csquad status` shows the same change
  (`status line +4.1k in 10 s`) instead of the trail; `status --json` has
  `change`, `changeText` and `changeSeconds` instead of `trail` and
  `trailSeconds`.
- The status line ledger is a tight table. The state, usage and age of a
  column are each as wide as their widest value, two cells apart, so one
  task reads `▎ ✓ T4 ✻ readme demo recording  done  290k  3m` with no
  holes. Up to four tasks always take one column, at any width; more take
  ⌈n/4⌉ columns (2 for 5–8 tasks, 3 for 9–12, 4 from 13) only when every
  title keeps 16 cells, else one column fewer and a `+N more` row. Each
  column is as wide as its widest row instead of a fixed share of the
  terminal (the thresholds 124, 187 and 250 are gone). A dotted leader
  follows a title only when it is 3 or more cells shorter than the
  longest title of its column. The change after the total keeps 7 cells
  (`+4.1k`, `+0`, `+?`, `idle 3m`), so a stall does not move the row. A
  `needs you` badge wider than the state and usage runs on before the age,
  which stays in its column; when the row does not fit, the reason is cut
  (to 16 cells), never the age. The README shows the ledger as SVG samples
  rendered from the real layout (`docs/images/ledger`).
- `csquad peek` shows the last assistant message in full, so `peek T12 -n 1`
  is the whole final report; older messages are still cut at 3000
  characters. `csquad status` says when it cut the latest message and which
  peek shows all of it.
- `csquad finish` names what blocks it and the next step. For each unmerged
  branch (or detached HEAD) it lists the commits not in the target, with
  short hash and subject, and counts those already in the target by patch
  ("2 commits already in main by patch (cherry-picked)"). A dirty worktree
  says "only untracked files: .pristine/ (14 files)", grouped by top-level
  path, apart from "modified tracked files: …". Every refusal ends with
  `csquad cancel T2` (keep the work, stop tracking it) and
  `csquad cancel T2 --discard` (delete it). `cancel` that keeps work names
  the worktrees and branches it kept.
- A branch whose commits are all in the target by patch (cherry-picked or
  rebased; `git cherry` shows only `-`) counts as merged, for `finish`,
  for cancel's nothing-to-lose check and for a `--merge` landing (which no
  longer rebases it). finish says "merged into main by patch
  (cherry-picked/rebased)" and deletes that branch with `git branch -D`,
  only while it still points at the commit verified. A merge commit, or a
  cherry-pick whose patch changed while resolving a conflict, keeps the
  branch unmerged.
- A task whose session was removed outside csquad (`gone`) and of which
  nothing is left (no worktree, no branch named for it) leaves the list by
  itself, from `status` or the status line: phase `cancelled`, note
  "removed outside csquad", with an event in the log. One with work left
  stays: `status` shows `gone` with the hint `csquad cancel T2`, and the
  status line row shows `gone` instead of `stopped`.
- The finish-task skill says how to keep only some commits (cherry-pick,
  then finish; then `cancel --discard` after asking the user), and that
  `csquad cancel` and `cancel --discard` are the ways to stop tracking or
  delete a task's work, never deleting it by hand. The task-status skill
  explains a `gone` row.

### Fixed

- A message held for your approval after a task was done (Claude Code lists
  the session as `state: done, status: waiting, waitingFor: "permission
  prompt"`) showed the task as `done` (or `ready`) in `status`, `wait` and
  the status line. It now shows `needs input`: a wait on the user comes
  before a finished state.
- `csquad cancel T3 --discard` on a task that a plain cancel already
  cancelled failed with "already cancelled", although cancel had printed
  that command. It now deletes the task's own worktrees and branches
  (unmerged ones included) and its session; an in-place task's checkout is
  never touched. A plain cancel of a cancelled task names the work that is
  left and the `--discard` command.
- The status line no longer loses the age at the right of a full row:
  Claude Code indents the status line and cuts a row longer than
  `COLUMNS-4` with `…`, and the ledger filled `COLUMNS-1`. Rows now fit
  `COLUMNS-4`. In a narrow cell the title shrinks first, then the usage
  cell steps down and the reason of a `needs you` badge is cut, then the
  title gives the rest.

- Windows: a detached csquad (a check, an advance) no longer opens a console
  window for every git, shell or agent it starts; they share its hidden
  console.
- Windows: finishing, cancelling or landing a task removes its
  `logs/T<n>.lock` (an open file cannot be deleted there).
- Windows: install and update leave no `bin/csquad.exe.old` unless the old
  binary was running.
- Windows: when the binary's path needs quotes (a profile path with a space)
  and lies below your profile, the status line, the Stop hook and the skills
  name it as `~/.csquad/bin/csquad.exe`: PowerShell, which Claude Code uses
  without Git Bash, cannot start a quoted program path.
- Windows: without Git Bash, your original status line and `cmd:` checks run
  in PowerShell, as Claude Code runs them, instead of `cmd.exe`.

## 3.0.0 - 2026-10-24

### Added

- Token usage in the status line (#41): working, fixing and merging rows show
  a trail of their last 8 refreshes on a log scale (`▁` to `█`, `·` for no
  change, `▪` for a change of unknown size), a stall after 2 minutes as dots
  and the idle time in amber; other rows show their total, faint (`326k`,
  `1.2M`, `?`). Urgent badges run across the cell; below about 52 columns it
  steps down after the title shrinks. Claude usage comes from the transcript
  (one usage per message id, fresh work = input + cache writes + output,
  cache reads never counted; read incrementally), Codex usage from the
  rollout's newest `token_count` event. The status line keeps 9 readings per
  task in `~/.csquad/cache` (readings less than 8 s apart are dropped) and
  gives reading usage at most 1 s of its 3.5 s budget.
- `csquad status` and `--json` show a usage block for each working task:
  fresh total, change since the last refresh, last activity, the parts,
  context fill and the trail.
- `csquad launch --fork` starts a task as a fork of the dispatching session,
  inheriting its conversation (Claude:
  `claude --bg --resume <full id> --fork-session`; Codex: `thread/fork`,
  with the worker rules in the first turn). Same engine only; refused
  without the dispatching session's id. A fork's usage leaves out the
  parent's, which its transcript or rollout carries over.
- Codex as an engine (#41, M2): Codex threads on Codex's app-server daemon
  as workers, reviewers and dispatchers; skills in `~/.agents/skills`.

- Orchestration (#41, M3): `launch --after T3,T4` (queued until they are
  done; the prompt gets what they left), `--check codex|claude[:MODEL]|cmd:COMMAND`
  with `--max N` fix rounds, `--merge [BRANCH]` (csquad rebases, fast-forwards
  the target, verifies and cleans up; a conflict goes to the worker first).
  New phases `waiting checking fixing merging ready needs_you failed`,
  `csquad retry`, the internal `hook claude-stop|codex-stop`, `check` and
  `advance`, a Stop hook in every Claude task's settings, a Codex Stop hook
  group in `~/.codex/hooks.json` when Codex is installed, and
  `~/.csquad/logs/events.jsonl` (one line per phase change).

### Changed

- **Breaking:** csquad is one Go binary (`cmd/csquad`, `CGO_ENABLED=0`)
  instead of the Node `tasks.js` and installer (#41). `csquad install` copies
  it to `~/.csquad/bin/csquad`, renders the skills with its absolute path and
  wraps the status line. Commands: `launch status peek wait send finish
  cancel statusline install update uninstall status-install`.
- Tasks are recorded in `~/.csquad/csquad.db` (SQLite) instead of being found
  by scanning transcripts and `launches/`; a task belongs to the session in
  its `parent_session` column, and numbering comes from the table.
- `csquad finish` verifies a task's branches are merged and its worktrees
  clean, removes them with git, then removes the session; `csquad cancel`
  stops a task and keeps unmerged work unless `--discard`; `csquad send`
  messages a live session's inbox or wakes a stopped one.
- The status line ledger sizes each column's title area to its longest title,
  so a single wide column no longer trails dozens of dots.
- Merged task settings are always written to `~/.csquad/settings/T<n>.json`
  (mode 600) and passed by path, including a user's inline `--settings`.
- Model routing lives in `~/.csquad/models.md`; `config.json` is gone.
- A csquad 1.x installation is not migrated: `install` asks for
  `npx csquad@1 uninstall` first.
- npm packaging (#41, M4): `npx csquad@latest install` runs the Go binary.
  The `csquad` package is a small Node shim with optional dependencies on
  `@shunl12324/csquad-{linux,darwin,win32}-{x64,arm64}`, which carry the
  binaries; nothing is downloaded at install time. `node scripts/build-npm.js
  VERSION` builds them, and a `v*` tag publishes them with npm trusted
  publishing (`.github/workflows/release.yml`; the GitHub release stays
  manual). `csquad update` run by the installed copy of an npm installation
  runs `npx --yes csquad@latest update`. `csquad COMMAND --help` prints the
  command's usage.
- Skills are slimmer and may be several files: create-task is a core of
  about 450 words plus `reference.md`, read on demand; one launch form (the
  prompt on stdin, a quoted heredoc or a PowerShell here-string, no draft
  files) and one way to send (`csquad send`, also for Claude tasks).
  create-task reads `models.md` before every launch.

### Fixed

- One engine failing no longer hides the other's tasks: `status` and the
  status line show what the other engines report, and `wait` fails only
  when the engine of a watched, unfinished task keeps failing.
- Worktrees and branches named for a task but older than it (csquad 1.x
  reused numbers) are reported by `finish` and `cancel` and never checked,
  merged or removed.
- The install manifest records csquad's text in Codex's `hooks.json`
  (`hooksText`) in every case, so uninstall removes exactly that; a file
  install created goes.
- In `fixing` and `merging`, a Codex worker's reply is dated by its turn's
  start, as Claude's is by the transcript.
- `launch` removes the lock and usage files of finished tasks.

## 1.4.0 - 2026-10-24

### Changed

- The status line lists tasks as a ledger instead of a grid of chips (#44):
  one row per task with a coloured rail and glyph, the id, the title with a
  dotted leader, the state (`needs input` as a filled badge) and how long the
  task has been in that state. Two to four tasks side by side from 124, 187
  and 250 columns; at most four rows, the last one `+N more` with the hidden
  tasks' glyphs and ids (or counts when they do not fit); rows never reach the
  terminal's last column; most urgent first. `NO_COLOR` is honoured. Claude
  Code reports no state-change time, so the status line remembers when each
  task entered its state.

## 1.3.3 - 2026-10-24

1.3.2 was tagged but never published: its Windows test run failed on the
order of the forwarded keys in `launch`'s stderr note, which is now sorted.
These are the 1.3.2 changes.

### Fixed

- Settings that `launch` changes (the `baseRef`, in-place isolation, forwarded
  provider variables) are always written to `csquad/settings/T<n>.json` (mode
  600) and passed by path, so a `-- --settings FILE` holding a token is no
  longer inlined into `claude`'s command line (#42). A `--settings` csquad does
  not need to change is passed through untouched.
- Provider variables are forwarded all or nothing: when the task's own
  `--settings`, the user's or the project's settings choose a provider
  (`ANTHROPIC_BASE_URL`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_API_KEY`,
  `CLAUDE_CODE_USE_*`, `CLAUDE_CODE_OAUTH_TOKEN` or `apiKeyHelper`), none of the
  launching session's are, so one provider's token and model mappings never
  reach another provider (#43). `launch` names the forwarded keys (never their
  values) on stderr.
- A removed task's settings file, which can hold a token, is deleted by
  `status` and the status line, not only by the next launch; `finish-task`
  runs `status` after removing a session (#43). Files written after the task
  list was requested are never pruned (a status line racing a launch deleted
  the new task's settings), and a session that launched nothing never prunes.

## 1.3.1 - 2026-10-22

### Changed

- Task worktrees branch from the local HEAD: `launch` adds
  `worktree.baseRef: "head"` to the task's `--settings` (Claude Code's default
  branches from `origin/<default branch>`, without unpushed commits), unless
  the user set a `baseRef` after `--`, in their settings or in the project's.
- `finish-task` removes a task's verified worktree and merged branches with git
  before `claude rm`, so `claude rm` has no unpushed commits to refuse. The
  `--discard-unpushed` / `--force-remove-worktree` values are used only when
  `claude rm` prints them; on Claude Code before 2.1.260, which has neither
  flag, the skill does not invent one (#39).
- Tasks use the launching session's provider. Claude Code's background
  service does not see the shell environment, so tasks failed with "Login
  expired" when the base URL and token were exported in the shell or set by an
  alias. `launch` copies `ANTHROPIC_*`, `CLAUDE_CODE_USE_*`,
  `CLAUDE_CODE_OAUTH_TOKEN` and `CLAUDE_CODE_SUBAGENT_MODEL` into the task's
  `--settings` env (the user's `--settings` env wins), written to
  `csquad/settings/T<n>.json` with mode 600 rather than the command line, and
  pruned once the task is gone. `config.json` `"forwardEnv": false` disables it.
- `install` and `status` warn when Claude Code is older than 2.1.260; the
  README states the requirement (#39).

## 1.3.0 - 2026-10-12

### Added

- `tasks.js launch --isolation none` starts a task that edits the main
  checkout in place, for example to show changes live through a running dev
  server (#38). It passes `--settings '{"worktree":{"bgIsolation":"none"}}'`
  (merged into any `--settings` given after `--`, inline or a file), since
  `CLAUDE_BG_ISOLATION` in the environment does not reach the session, and
  gives the worker in-place rules: no worktrees or branches, leave unrelated
  changes alone, do not commit unless asked. `--isolation worktree` or no flag
  keeps the current behaviour. `create-task` and `finish-task` describe it.
- `tasks.js launch --no-worker-rules` leaves out `worker.md`, for prompts that
  bring their own rules.

## 1.2.2 - 2026-10-12

### Fixed

- Tasks launched from a background Bash call (#37), or through a variable such
  as `node $T launch` (#36), now count as launched by the session.
  `tasks.js launch` records the child under the launching session's id
  (`CLAUDE_CODE_SESSION_ID`) in `csquad/launches/`, and the status line,
  `status`, `peek` and `wait` read that record as well as the transcript
  marker, including records of the sessions a continued session came from.
  Uninstall removes the records.

## 1.2.1 - 2026-10-10

### Fixed

- The `finish-task` skill's description had an unquoted `: ` in its YAML
  frontmatter, so the frontmatter did not parse. Inline code that wrapped across
  lines in `finish-task` and `worker.md` is fixed, and a test now checks every
  skill's frontmatter and inline code.

## 1.2.0 - 2026-10-10

### Added

- `tasks.js launch … -- CLAUDE_ARGS` passes the arguments after `--` to
  `claude --bg` unchanged (for example `--model`, `--effort`, `--settings`),
  overriding the configured model and permission mode. Arguments csquad sets
  itself (`--name`, `--bg`, `--append-system-prompt`, `--print`, `--resume`)
  are refused. `create-task` documents it.

## 1.1.0 - 2026-10-04

### Added

- Windows support. `tasks.js` finds `claude` on `PATH` (`claude.exe`, or the
  program behind npm's `claude.cmd`), runs your original status line through Git
  Bash like Claude Code does, and keeps launch prompts under the 32 K command
  line limit. The status line wrapper is `node "C:/path with spaces/tasks.js"
  statusline`, valid in every shell; settings and manifest writes retry on
  EPERM/EBUSY/EACCES; CI now runs on Windows too. The package no longer
  declares itself unsupported on `win32`.
- `tasks.js launch --prompt-file FILE [--rm-prompt]` reads the prompt from a
  file instead of stdin. `create-task` uses it, so launching no longer needs a
  bash heredoc and works in PowerShell and cmd.exe.
- `watch-task` skill and `tasks.js wait T12 [T13 ...]`, an opt-in monitor. It
  polls `claude agents` every 10 s without the model and exits when a named task
  needs input, finishes, fails, is stopped or is removed (a new state must show
  on two consecutive polls), printing one line per task. It never runs unless
  you ask to watch tasks; `create-task` still does not poll by default.

### Changed

- `uninstall` never removes a `skills/` or `csquad/` symlink or junction, even
  an emptied one (on Windows removing an empty directory link deletes the link).
- The skills and `worker.md` give the PowerShell equivalent of the `rm -rf`
  rule, and `watch-task` allows PowerShell's `run_in_background`.
- `worker.md` and `finish-task` tell Claude to use literal paths (or
  `${VAR:?}`) with `rm -rf`. Claude Code otherwise stops even bypass-mode
  sessions for a human to approve the command, and denies it when nobody
  answers.
- `watch-task` finishes a watched task on its own when it is done, but only if
  the user explicitly said to accept or merge it themselves; otherwise it asks.
- `finish-task` with no task named finishes every done task of the session,
  skipping those still working or needing input.

### Fixed

- Task states now follow agent view. A session that finished its turn is
  reported by Claude Code as state "working" with status "idle"; it showed as
  working in the status line, `status` and `wait`, and now shows as idle.
  Status `shell` counts as working, `waiting` as needs input, and `crashed` as
  failed.
- The status line no longer blanks out now and then when many tasks run.
  Claude Code clears it whenever the command fails, times out or prints
  nothing; the user's own line and `claude agents` now run in parallel, and a
  part that fails shows its last good output (up to 2 minutes old) instead.

## 1.0.2 - 2026-10-01

### Changed

- The status line, `status`, `peek` and the skills are scoped to the tasks the
  current session launched. `peek` refuses another session's task unless you
  pass `--all`; `status --all` still lists every task.
- Only launches recorded in this session's own `tasks.js launch` output count;
  a `launched T…` line that is quoted, pasted or shown by `peek` or `status`
  no longer adds a task to the status line.
- The status line scans transcripts incrementally, reading only what was
  appended since the last refresh.
- Prompts over 60 KB are passed to the task through a file under
  `csquad/prompts/`, instead of failing on the per-argument size limit.
- The README is rewritten, with a new banner.

### Fixed

- The status line no longer runs `claude agents` every 5 s in sessions that
  never launched a task, and gives up after 4 s.
- A failed `launch` left `launch.lock` behind and blocked the next launch for
  minutes. The lock now records its pid and is taken over once that process is
  gone.
- Long `claude agents --all` lists (over 1 MB) no longer break the status line
  and `launch`.
- `status` and `peek` no longer crash before `~/.claude/projects` exists.
- Control characters in task titles are stripped from the status line.
- CRLF and one-line `settings.json` files keep their format on install and
  uninstall; `npx csquad status` tolerates an invalid `settings.json`.
- `finish-task` also checks a detached worktree `HEAD` before deleting.
- Taking over a launch lock left by a crashed launch is atomic, so concurrent
  launches never get the same task number.
- A dangling `settings.json` symlink is written through instead of being
  replaced by a regular file; unsafe symlink layouts are refused before any
  change.
- Uninstall removes the prompt files and lock directory `tasks.js` writes.
- Several tasks launched by one Bash call all show in the status line, not
  only the first.

## 1.0.1 - 2026-10-01

### Fixed

- Tasks disappeared from the status line after switching back from agent view,
  until the next message. Claude continues the conversation in a new session
  whose transcript does not exist yet; the status line now follows the
  `continued-in` record back to the previous transcript.

## 1.0.0 - 2026-10-01

### Changed

- **Breaking:** the Go CLI (`csquad add`, `ls`, `peek`, the dispatcher, the
  SQLite queue and the console) is replaced by a small zero-dependency Node
  package. `npx csquad@latest install` copies four Claude Code skills
  (`create-task`, `task-status`, `message-task`, `finish-task`), `tasks.js` and
  the worker rules into `$CLAUDE_CONFIG_DIR` (default `~/.claude`) and wraps your
  status line to show task chips without losing it. `update`, `uninstall` and
  `status` manage the installation. See the README.
- Tasks are plain `claude --bg` sessions named `T1 · title`; there is no queue,
  slot limit or database any more. The old database at
  `~/.local/share/csquad/csquad.db` is no longer used and can be deleted.

### Removed

- Homebrew and APT distribution are discontinued; npm is the only channel.
  Uninstall old copies with `brew uninstall csquad` or `apt remove csquad`.

## Earlier releases (Go CLI)

## v0.13.1 — 2026-09-30

### Fixed

- The Homebrew formula test checked for the configuration file the old
  `csquad config` created, so v0.13.0 never reached Homebrew. It now checks
  that `csquad config` reads `$XDG_CONFIG_HOME/csquad/config.toml`.

## v0.13.0 — 2026-09-30

### Changed

- **Breaking:** C Squad is now a task queue on top of Claude Code background
  sessions. `csquad add` queues a task, a dispatcher starts it with
  `claude --bg` while fewer than `slots` sessions (default 6) are working or
  waiting for input, and `csquad ls` / `csquad peek` show live state and recent
  messages. Bare `csquad` starts `claude` with the console prompt appended. See
  `docs/redesign.md`.
- `csquad finish TASK` removes a task's session, leftover worktrees and the
  branches it worked on once they are merged, with or without worktrees.
  `ls` hides finished and cancelled tasks (`--history` shows them) and deletes
  them after 7 days.
- Tasks live in one global SQLite database at `~/.local/share/csquad/csquad.db`.
  Session state is read live from `claude agents --json` and never stored.

### Removed

- The tmux runtime, team UI, members, Master, messaging, stall detection,
  recovery, pinning, profiles, Codex support, task worktrees, merge and evidence
  gates, and every command built on them. tmux and procps are no longer package
  dependencies. Existing `.csquad/` team directories are left untouched.

## v0.12.11 — 2026-09-28

### Fixed

- Stop legacy runtime and member processes before switching their team to the
  installed executable, so an older build cannot rewrite the migrated state.
- Record each team's installed build and reject commands from a replaced build
  until the team is stopped and resumed. Refuse `csquad update` while a known
  team using the installed executable is active.
- Reject temporary `npx` and `go run` executable paths for persistent teams.

## v0.12.10 — 2026-09-28

### Changed

- Start and resume teams with the installed csquad executable instead of
  making a new private binary copy. This keeps the executable path stable for
  antivirus allowlists. Existing teams with private copies move to the installed
  path with `stop` and `resume`; team commands no longer launch those copies
  through the installed build. Their saved work remains intact.
- Warn that teams using the installed executable should be stopped before a
  package update and resumed afterward.

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
