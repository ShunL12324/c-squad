# Using C Squad

List saved teams with `csquad list`. Run `csquad start research` to create a new
team. An existing name is always an error, whether the team is running, stopped,
or interrupted; rejection does not clean up or resume that team. Bare `csquad`
also creates a new team, and `--name NAME` remains supported.
Use `csquad resume research` to continue a stopped or interrupted team,
`csquad attach research` to enter a running team, or
`csquad --team-name research member attach reviewer` to open one member.
`csquad stop research` stops processes while keeping recoverable work.
`csquad recover research` restarts only master in an active team from an outside
terminal; it does not replace whole-team `resume`.

Start a team in your project with `csquad start --engine claude` or
`csquad start --engine codex`. This opens the Master session. Tell it what you
want built and how you want work divided; it recruits and coordinates members.

The task board is primarily for agents. Master uses it to track ownership,
progress, blockers, and review/test evidence. You can ask Master for a summary
or inspect the underlying state with `csquad board`.

| Action | Shortcut or command |
| --- | --- |
| Open a member with the mouse | Click its name in the left sidebar |
| Switch to the previous or next member | `Alt+Up` / `Alt+Down` |
| Return to Master | `Ctrl-b 0` |
| Open a numbered member | `Ctrl-b 1` … `Ctrl-b 9` |
| Detach and leave the team running | `Ctrl-b d` |
| Start without taking over your terminal | `csquad start --detach` |
| Inspect progress | `csquad board` |
| Restart a member | `csquad member restart alice` |
| Stop the team | `csquad stop` |
| Recover an interrupted team | `csquad resume` |

Outside a team session, use `csquad --team-name research COMMAND` or
`csquad --state-dir /path/to/team COMMAND`. The existing `--team DIR` still means
a state directory. Supply only one selector: positional team name, `--name`,
`--team-name`, `--state-dir`, or `--team`. `start` accepts only a new positional
name or `--name`; it never selects existing state. With no selector, existing-team
operations use the bound session, then the current project's last team, then the
legacy global last team. Names match exactly. Team state is normally in
`.csquad/teams/<name>/`. Member sessions cannot select another team or override
bound caller/generation identity. Completion uses the same selection rules.

Member instructions use `csquad COMMAND`, with the selected executable made
available on the member's PATH. `--member` identifies the caller, not an attach
target. Legacy `attach --name TEAM MEMBER` and bound-session `attach MEMBER`
remain supported; prefer `member attach MEMBER` for an unambiguous member target.
Team shortcuts do not modify `~/.tmux.conf`.

Mouse support is enabled only in team sessions. Click a member in the sidebar to
switch sessions, use the wheel to scroll, and drag pane borders to resize.
Native agents control mouse behavior inside their own interfaces.
In most terminals, hold Shift while dragging to select text without sending
mouse events to tmux.

Master process exit stops the team; detaching does not. Recovery preserves
worktrees and uncommitted changes. `resume --fresh` starts new native sessions
when old session history is unavailable. A machine shutdown cannot execute
cleanup immediately; the next start/resume checks for remaining state.

## Configure only what you need

The first configuration load creates `~/.config/csquad/config.toml` with detailed
comments. Run `csquad config` to inspect the effective values and configuration
path. An optional project `.csquad.toml` overrides user settings by field.

```sh
csquad start --engine claude --model opus
csquad start --engine codex
```

The Master recruits members through `csquad member add`, supplying their name,
engine, and role description. You can describe the team you want in plain language.

Environment overrides work without account profiles:

```sh
csquad start --env CODEX_HOME=/absolute/path/to/codex-config
```

Precedence: inherited environment → configuration `env` → `start --env` →
`member add --env`. Running teams retain their startup configuration. On recovery, changes to
configured environment defaults apply to members that inherited those defaults;
member-specific overrides remain intact. Changing `CODEX_HOME` starts a fresh
Codex conversation in that configuration directory while preserving the task
ledger and recovery handoff. Member
limits are configurable; there is no conversation-turn cap. A member's
`generation` identifies its current process incarnation, not an iteration limit.

`previous_member_key` and `next_member_key` control the member switch shortcuts;
they default to `M-Up` and `M-Down` (Alt/Option with the arrow keys). See
[member switch keys](#member-switch-keys-and-your-agent) for what binding them
takes away from your agent and how to release them.

**Agents bypass native approval prompts by default.** Set
`bypass_permissions = false` to retain approvals. With bypass enabled, C Squad
also confirms Claude's startup trust dialog for the member's selected working
directory. Claude saves its normal project trust record; C Squad does not set a
sandbox environment variable. This does not log in, supply
account quota, or override organization policy. Project files such as untracked
MCP configuration are not automatically copied into worktrees.

## Help and completion

```sh
csquad --help
csquad task create --help
csquad member add --help
```

Homebrew and APT install Bash, Zsh, and Fish completions automatically. In a new
terminal, type `csquad sta` and press **Tab** to complete `csquad start`. No
`csquad completion` setup command is needed. Your shell must have its normal
completion system enabled (for example, Oh My Zsh already enables Zsh completion).

Completion includes commands, flags, and IDs from the current team. It reads the
ledger without waking agents. `csquad question request` is a team escalation command
(the old `help request` alias remains available);
use `--help` or `csquad usage` for command documentation.

### npm, npx, and manual installs

These channels ship the completion scripts but cannot enable them: the npm
package runs no lifecycle scripts, and no channel edits your shell
configuration. Enable completion yourself, with or without Homebrew.

**Current shell only.** Nothing is written to disk, and the effect ends with the
shell:

```sh
source <(csquad completion zsh)     # bash: source <(csquad completion bash)
csquad completion fish | source     # fish
```

**Persistently.** `csquad completion install` writes the script for one shell
into a directory you own, defaulting to your login shell and to
`$XDG_DATA_HOME` / `$XDG_CONFIG_HOME` (`--shell` and `--dir` override both). It
rewrites the file only when the content changed, so repeating it is harmless,
and it prints the remaining step instead of performing it:

```sh
csquad completion install
csquad completion status            # installed files, and the check for each shell
```

| Shell | Default target | Remaining step |
| --- | --- | --- |
| Bash | `~/.local/share/bash-completion/completions/csquad` | none, but bash-completion v2 must be installed: it reads that directory and provides helpers the script calls |
| Zsh | `~/.local/share/zsh/site-functions/_csquad` | add the printed `fpath=(...)` line to `~/.zshrc` |
| Fish | `~/.config/fish/completions/csquad.fish` | none |
| PowerShell | `~/.local/share/csquad/csquad.ps1` | source it from `$PROFILE` |

Paste the line the command prints rather than retyping it: it quotes the
directory, which matters when the path contains a space, where an unquoted entry
would silently become two `fpath` elements.

**Zsh ordering matters.** The `fpath` entry must come *before* the command that
runs `compinit`; with Oh My Zsh, put it above `source $ZSH/oh-my-zsh.sh`, which
calls `compinit` itself:

```sh
fpath=(~/.local/share/zsh/site-functions $fpath)
source $ZSH/oh-my-zsh.sh            # or: autoload -Uz compinit && compinit
```

**Then restart the shell.** Completion is read at startup, so the current shell
will not pick it up. If a new terminal still does not complete, Zsh is using a
cached index: `rm -f ~/.zcompdump*` and open another terminal.

**Verify** in that new terminal with `csquad sta` + **Tab**, or query the shell
directly — `print -r -- ${_comps[csquad]:-missing}` in Zsh (prints `_csquad`),
`complete -p csquad` in Bash, `complete -c csquad` in Fish. `csquad completion
status` reports the files on disk; it cannot see a running shell's state,
because no shell exports its `fpath` or its loaded completion functions.

**nvm and upgrades.** The installed script locates `csquad` on `PATH` when you
press Tab and lives in your data directory rather than under the Node prefix,
so switching Node versions with `nvm use`, upgrading Node, or running
`npm install -g csquad@latest` leaves it working. Re-run
`csquad completion install` only to pick up completions for newly added
commands; `csquad completion status` reports `differs` when the file no longer
matches the installed binary.

## Compatibility and limitations

- **Early-stage software.** Linux has been exercised with real Claude Code/Codex
  sessions. macOS and Linux ARM64 builds exist; macOS/WSL runtime acceptance is
  still pending. Native Windows is not supported.
- **Native engine compatibility matters.** Prior manual checks used tmux 3.4,
  Claude Code 2.1.276, and Codex 0.154.0. Engine updates may require adapter changes.
- **Messages can be retried.** Delivery is not exactly-once; a transport acceptance
  is different from an agent acknowledgement. Persistent failures appear on the board.
- **Local state stays local.** `.csquad/` holds recovery data and should not be
  committed. Use local disk for the SQLite ledger. A per-team runtime handles
  delivery and cleanup; no system service is installed.
- **Upgrades are explicit.** Stop running teams before replacing the CLI binary.

## How tasks are coordinated

Members share a persistent task board and can send direct messages or broadcasts.
Claude receives messages in its native peer envelope; Codex receives a compact
sender header. Both use the same task records, acknowledgments, and reply routing.
Coordination instructions are injected into session context, not appended to every
message. Claude may still display its own native peer notice.
They report meaningful milestones and ask Master for help when blocked. Master
brings questions that need a human decision back to you.

A code task gets a Git worktree, even when it has only one developer. Its owner
writes the code; other members review or test it. Separate implementation tasks
use separate worktrees. Code tasks require a Git repository with an existing
commit; research tasks can run without Git.

A code task worktree is always created in the team repository. It does not follow
a member's `--cwd`, so a member working in a different repository cannot submit
its commits: `csquad` warns about this when the task is created or assigned. When
work has genuinely landed in another repository, Master can close the task on that
commit:

```sh
csquad task close-external T7 --repo /path/to/other-repo --sha 9f3c1ab \
  --reason 'Work was pushed from the member repository'
```

That records the commit, the reason and what was not verified, and frees the
owner. It is not a merge: nothing is fetched into the team repository, and the
task is shown as closed externally rather than merged.

Review and test evidence refer to a specific candidate commit. Master approves
and performs the merge through C Squad. If you want a human checkpoint, tell
Master to ask you before merging.

C Squad injects coordination instructions into native engine sessions. No extra
collaboration skill or MCP server is required. Native MCP configuration and
account authentication remain under each engine's control. The application uses
a local SQLite ledger and a per-team runtime; it installs no system service.

## Member colors

Give collaborators the same status-bar color to make a task group easy to spot:

```sh
csquad member add developer --engine codex --role developer --color mint
csquad member add reviewer --engine claude --role reviewer --color mint
```

Colors are visual labels, not task assignments or status indicators. Master is
prompted to use matching colors for collaborators, but this is optional. Without
`--color`, a random color is selected and retained across restarts and recovery;
duplicates are allowed. Use `csquad start --color blue` to color Master as well.

Available colors: `red`, `orange`, `amber`, `yellow`, `lime`, `green`, `mint`,
`teal`, `cyan`, `sky`, `blue`, `indigo`, `violet`, `purple`, `pink`, and `rose`.
The current member has a filled label; other members use colored text. Terminal
color themes can affect their appearance. `--color` supports shell completion.

## Team panels

![Members, native agent terminal, and task details](assets/workspace.png)

On wide terminals, new teams open both the member sidebar and task board beside
the native agent terminal. `Alt+Up` and `Alt+Down` switch to the previous or
next member in one keypress, from anywhere in the session; the list is cyclic,
so `Alt+Down` on the last member returns to Master. You can also click a member,
or focus the sidebar first and then select with the arrow keys and press Enter:
the sidebar is a separate tmux pane, so plain arrow keys reach it only while it
holds focus, which is why the Alt shortcuts exist. Each member
block shows its engine, working directory, status, and assigned task IDs. Long
paths retain their trailing directory components; task details show the full workspace. The agent's
terminal remains a native tmux pane; click it to resume typing.

| Action | Shortcut or command |
| --- | --- |
| Switch to the previous or next member | `Alt+Up` / `Alt+Down` |
| Toggle the member sidebar | `Ctrl-b b` |
| Toggle the task panel | `Ctrl-b t` |
| Show members and tasks | `csquad ui` |
| Show only tasks beside the terminal | `csquad ui --view tasks` |
| Hide both panels | `csquad ui --view hide` |
| Collapse the task panel | Top-right **×**, `q` or `Esc` |
| Return to Master | Click the pinned Master card |

### Member switch keys and your agent

The team binds `Alt+Up` and `Alt+Down` in its own tmux key table, which consumes
them before the pane sees them. **The agent CLI in the engine pane no longer
receives those keys.** For Claude Code that costs its `meta+up` / `meta+down`
bindings, which move through the diff file list and jump the message selector to
top or bottom; both actions keep their other default bindings (`ctrl+up` /
`ctrl+down`, and `shift+up` / `shift+down` for the message selector), so nothing
becomes unreachable. Codex does not bind these keys. tmux treats Alt and Meta as
one `M-` namespace, so it cannot bind one encoding and pass the other through.

Remap or release them in your configuration:

```toml
previous_member_key = "C-M-p"   # any tmux key name, for example M-Up, C-M-n, F5
next_member_key = "C-M-n"
```

Set either to an empty string to leave that key to your agent and navigate with
`Ctrl-b 0`–`9` or the sidebar instead. A key name tmux does not recognise is
rejected when the configuration loads rather than producing a binding that never
fires.

On macOS, Terminal.app sends Option as an accent composer unless **Use Option as
Meta key** is enabled in its keyboard settings; without it the Alt shortcuts
never reach tmux. iTerm2 and most Linux terminals send Alt correctly.

Each member card names the Git branch of that member's OWN working directory -
`Git main`, `Git detached 4f2a1b9`, or a branch with `wt` when the directory is a
linked worktree. It is the member's directory, not the repository its task is
bound to, which is what matters when a member works in another repository. Long
branch names keep their trailing segment; `csquad member inspect NAME` prints the
untruncated value under `git`. Nothing Git-related is written to the ledger, and
the panel only reads repository state.

When the roster is longer than the sidebar, a position indicator appears in the
right-hand gutter showing how far through the list you are. It is absent when
every member already fits.

The workspace header displays the C Squad mark and team name.
The bottom footer has clickable **Tasks** and **Detach** buttons.
Tasks toggles the right panel; the member sidebar stays open. Detach disconnects only your terminal; the
team keeps running. Button labels include their keyboard shortcuts.

Master stays pinned above the scrolling member list, marked with a diamond.
Member names always use their assigned colors, including unselected members.

The task panel opens on **Active**, which includes pending, working, and review tasks.
Completed tasks move to **Done**. Click either filter or use the left/right arrow
keys to switch; both filters show their task counts.

The task panel shows the recorded task phase, owner, collaborators, acceptance
criteria, latest progress report, blockers, checkpoints, and review/test evidence.
Tasks appear as vertically stacked cards with their status, owner, and latest
progress. Cards show structured milestones, including checkpoints awaiting approval.
Click **View details** or press Enter to read the full task in the panel. Escape
or **Back to tasks** returns to the list; the wheel and Page Up/Down scroll details.
Press `o` to open the task owner's terminal. The right panel is dedicated to
tasks. Questions are handled through Master; this panel does not assign tasks,
approve merges, or infer completion from terminal output.

At 150 columns or wider, both panels fit beside the terminal. Between 90 and 149
columns, the member sidebar stays visible and Tasks opens in a popup. Below 90 columns,
side panels are hidden; the panel shortcuts open a temporary popup instead.
Expand the terminal to restore the chosen layout. Pane borders can be dragged to
adjust widths; resizing the terminal restores the standard sidebar widths. The header stays three rows high. As with normal tmux panes, clients attached to the same session
share its layout and panel selection.

### Member working directories

For a team spanning several projects, set each member's actual startup directory:

```sh
csquad member add api-reader --engine codex --cwd /path/to/api --role "API researcher"
csquad member restart api-reader --cwd /path/to/api
```

The directory must exist. Relative paths resolve from the calling directory.
Without `--cwd`, creation uses the `--task` workspace when available, otherwise the
team root; restart and replace retain the existing member directory.
Restart resumes the conversation. Replace starts a new conversation.

The mouse wheel scrolls panel content without changing selection. Click a member
to switch terminals; arrow keys move the selection and Enter opens it. Master
remains pinned while the member list scrolls.

## Text inputs, query output, and compatibility

Use `question request|list|answer` for escalation and `message reply MESSAGE` for
replies. Existing `help request|list|answer` and top-level `reply` retain their
behavior, including question blockers and reply deduplication. `sync` starts an
active team's runtime when needed and retries delivery; `reconcile` separately
reconciles durable merge intents.

Commands accepting `--text`, `--summary`, `--instructions`, or `--description`
also accept the corresponding `--FIELD-file FILE`. A filename of `-` reads stdin.
Inline and file forms are mutually exclusive; only one field may consume stdin.
File content is preserved exactly, including newlines; empty files are rejected.
Required text/summary fields accept either form. For example:

```sh
csquad message send reviewer --text-file ./review-request.txt
csquad task submit T7 --summary-file ./result.md
csquad question request --task T7 --text-file - < ./question.txt
csquad member add reviewer --instructions-file ./responsibilities.md
csquad task create 'Fix login' --acceptance 'Regression passes' --description-file ./issue.md
```

`list`, `board`, `member list|inspect`, `task list|inspect`, `message inbox`, and
`question list` accept `--output json|table`. Omitting the flag preserves existing
output: saved-team `list` uses a table; other queries use JSON. JSON result shapes
are unchanged. List tables show one resource per row with its identity, state, and relevant
owner, sender/recipient, or text columns. Board tables show team status plus
members, tasks, and questions. These are concise projections; use JSON for full
history and metadata. Inspect tables retain detailed fields with nested JSON.
Embedded control characters are escaped to preserve table rows. Output format flags do not apply to interactive startup, attach,
or completion script generation. Usage failures exit 2; operational failures exit 1.

Hidden runtime commands remain callable at their original paths. The additive
`_internal COMMAND` aliases normalize to the same operation before identity and
permission checks; the namespace itself grants no extra authority. Native
`run-engine -- ...` arguments remain opaque, including through its internal alias.

Saved-team removal is separate from stopping. From an outside terminal, inspect
`csquad team remove NAME --dry-run` before `csquad team remove NAME`. Removal
requires stopped state, no remaining processes or sessions, no pending merge,
and clean, fully merged owned worktrees. It removes eligible task worktrees and
the saved ledger, preserves Git branches, and refuses unknown or external
worktree paths, unknown payload, or nested repositories. Ignored files are listed
by dry-run and prevent removal unless `--discard-ignored` is supplied explicitly.
There is no force flag to discard uncommitted or unmerged work. A leftover tmux
socket from a crashed server is accepted only when its endpoint is absent or
refuses connections. Reachable sockets still require successful tmux session
inspection; permission, timeout, and protocol errors prevent removal. Inspection
does not delete the stale socket.

Every submission has an immutable identity. For formal research/non-code review,
use `task evidence TASK --submission ID --kind review --passed true --summary TEXT`.
Code evidence retains exact `--sha COMMIT` compatibility. See
[task submissions and evidence](task-evidence.md) for revision fencing, retries,
and approval policy.

## Member command shells and PATH

C Squad puts a generation-specific `csquad` link on each member's PATH, pointing
to the executable selected for the team. Run team commands in a **non-login
shell** to keep that PATH precedence. In Codex, set the `exec_command` tool's
`login` argument to `false`, for example:

```json
{"cmd": "csquad board", "login": false}
```

Use the same setting for every team operation, including after restart or resume.
Do not wrap the command in a login shell. In native Codex testing, default login
execution retained the private directory but put a global installation ahead of
it; `login: false` selected the private link. The exact shell setup or snapshot
responsible for that difference was not established. Default login execution
therefore does not guarantee the team's selected executable. The non-login
invocation requires no absolute CLI path, manual PATH override, or global shell
configuration change.
The bound team, member, and generation variables still identify the caller.

To check resolution in the same non-login tool shell, run `command -v csquad`
and `csquad version`. The first command should select the current member's
`runtime/<member>/<generation>/bin/csquad` link.
