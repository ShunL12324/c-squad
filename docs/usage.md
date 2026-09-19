# Using C Squad

List saved teams with `csquad list`. Run `csquad start --name research` to create
a team, resume it after stopping, or enter it if it is already running.
Use `csquad attach research` to enter a running team, or
`csquad attach --name research reviewer` to open one member.

Start a team in your project with `csquad start --engine claude` or
`csquad start --engine codex`. This opens the Master session. Tell it what you
want built and how you want work divided; it recruits and coordinates members.

The task board is primarily for agents. Master uses it to track ownership,
progress, blockers, and review/test evidence. You can ask Master for a summary
or inspect the underlying state with `csquad board`.

| Action | Shortcut or command |
| --- | --- |
| Open a member with the mouse | Click its name in the left sidebar |
| Return to Master | `Ctrl-b 0` |
| Open a numbered member | `Ctrl-b 1` … `Ctrl-b 9` |
| Detach and leave the team running | `Ctrl-b d` |
| Start without taking over your terminal | `csquad start --detach` |
| Inspect progress | `csquad board` |
| Restart a member | `csquad member restart alice` |
| Stop the team | `csquad stop` |
| Recover an interrupted team | `csquad resume` |

Outside a team session, use `csquad --team /path/to/team COMMAND`; team state is
normally in `.csquad/teams/<name>/`. Team shortcuts do not modify `~/.tmux.conf`.

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
ledger without waking agents. `csquad help request` is a team escalation command;
use `--help` or `csquad usage` for command documentation.

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
the native agent terminal. Click a member
or select it with the arrow keys and press Enter to open its session. Each member
block shows its engine, working directory, status, and assigned task IDs. Long
paths retain their trailing directory components; task details show the full workspace. The agent's
terminal remains a native tmux pane; click it to resume typing.

| Action | Shortcut or command |
| --- | --- |
| Toggle the member sidebar | `Ctrl-b b` |
| Toggle the task panel | `Ctrl-b t` |
| Show members and tasks | `csquad ui` |
| Show only tasks beside the terminal | `csquad ui --view tasks` |
| Hide both panels | `csquad ui --view hide` |
| Collapse the task panel | Top-right **×**, `q` or `Esc` |
| Return to Master | Click the pinned Master card |

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
