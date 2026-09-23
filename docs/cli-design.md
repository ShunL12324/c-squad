# Team command design

C-Squad uses a team as the durable unit: its name selects a ledger, members,
messages and tasks. A member has its own native agent conversation and tmux
session; a task ID (for example `T7`) identifies work, not a terminal.

| Intent | Recommended command | Existing equivalent / behavior |
| --- | --- | --- |
| Create a named team | `csquad new -s research` | `csquad start research` or `csquad start --name research` |
| Create an automatically named team | `csquad new` | `csquad start` or bare `csquad` |
| List saved teams | `csquad list` | Shows running and stopped teams |
| Enter a running team | `csquad attach research` | Does not restart processes |
| Select a member | `csquad --team-name research member attach reviewer` | Legacy `csquad attach --name research reviewer` |
| Resume a stopped/interrupted team | `csquad resume research` | Restores team processes and conversations from the ledger |
| Repair master in an active team | `csquad recover research` | Restarts only master; run from an outside terminal |
| Stop a team | `csquad stop research` | Keeps the ledger and work available for resume |

`new -s` follows tmux's creation spelling. It creates a C-Squad team, not an
arbitrary tmux session. `-s` is a shorthand for `--name` on creation commands
(including `start` and bare `csquad`). Creation flags such as `--profile`
and `--detach` work identically through either spelling. Existing
names fail with guidance to attach or resume; creation never replaces a team.

Existing targets use positional names on lifecycle commands, or the global
`--team-name NAME` selector for other commands. `--team DIR` and its alias
`--state-dir DIR` select a state directory, not a name. Specify a target once;
creation rejects existing-team selectors. There is intentionally no global
`-s` or tmux `-t`: member names, team names and task IDs remain distinct.
Legacy `attach MEMBER` inside a bound/explicit team remains supported; prefer
`member attach MEMBER` when selecting a member to avoid context ambiguity.

`resume --fresh` keeps the team ledger but starts new native conversations;
`recover --fresh` replaces master's conversation only. Neither is an attach.
Omitting the team on existing lifecycle commands keeps the current/default
team behavior. Bare `csquad` always creates a new team.

Human commands normally need no identity flags. Agents use their bound
runtime identity; `--member` and `--generation` are runtime compatibility
parameters, not team selection or session-switching shortcuts. A conflicting
bound identity is rejected. JSON defaults and existing script commands remain
unchanged, with no deprecation in this increment.

`--help` recommends one spelling per operation: `new`, `question` and
`message reply`. The compatible `start`, `help request|list|answer` and
top-level `reply`, and the `--team`, `--state-dir`, `--member` and
`--generation` selectors, are hidden from help and completion but parse and
behave exactly as before.

Implementation is additive: document the lifecycle, add `new` and the creation
shorthand, then test shared dispatch, errors, help and generated shell
completion. `--help` and `usage COMMAND` describe commands without starting
processes. Completion suggests existing team names only for existing targets;
a new name is free text. Additional tmux aliases are deferred until needed.
