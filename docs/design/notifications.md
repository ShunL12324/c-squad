# Native notifications

Status: proposal for 3.1.0.

## Goal

csquad tells the user when a task needs them or is finished. The user gets
the notification while they work in the master session. csquad stays one
binary plus skills: no service, no GUI, no separate app.

## Events

csquad sends a notification when a task changes into one of these states.
It sends each one once per change.

| Event         | When                                                        | Default |
|---------------|-------------------------------------------------------------|---------|
| `needs input` | the session asks a question, shows a permission prompt, or holds a message | on |
| `needs you`   | the task is in the phase `needs_you`                        | on      |
| `ready`       | the check passed (phase `ready`)                            | on      |
| `done`        | a task without a check or a merge ended its turn (idle or done) | on  |
| `failed`      | the session failed, or the phase is `failed`                | on      |
| `merged`      | `--merge` landed the work (phase `done` after a landing)    | on      |

csquad does not send a notification while a task works, for usage changes,
or for what the user did (`finish`, `cancel`, `retry`).

## Content

- Title: `T2 · fix login redirect`.
- Body: the state, then one detail:
  - `needs input`: the first line of the last assistant message.
  - `needs you`: the reason (the note).
  - `done`: the first line of the final message.
  - `merged`: the target branch.
- The body is at most 160 characters. It never holds settings, tokens or
  the prompt.

## Where it fires

All of csquad's short processes observe tasks: the status line, the Stop
hooks (through the detached `advance`), `wait`, `check`, and the
`AdvanceAll` of each command. Two places record a state:

1. `App.observe`: each engine observation of a task's session. The view
   (`view(t, live)`) gives the state.
2. A phase change that csquad writes (`flow.Mover.Apply`, `App.fail`):
   `ready`, `needs you`, `failed`, `merged`.

Both places call one function, `App.notice`. It claims the event in the
database and, if the claim wins, starts a detached
`csquad notify send T12 EVENT`. The detached process reads the task, makes
the text, and shows the notification. The status line only writes one row
and starts one process, so it stays in its 3.5 s budget.

The Stop hook also starts `advance --stopped` for a task without a check
or a merge, so `done` comes also when no status line runs (a Codex master).

## Deduplication

Migration 3 adds three columns to `tasks`:

- `notified`: the last observed event class (`""` for a quiet state such
  as working).
- `notified_at`: the time of that observation (unix ms).
- `notified_phase`: the `updated_at` of the phase that the last phase
  event (`ready`, `needs you`, phase `failed`, `merged`) was for.

The claim is one compare-and-set:

```sql
UPDATE tasks SET notified = :class, notified_at = :at, ...
 WHERE id = :id AND phase = :phase AND updated_at = :version
   AND notified != :class AND notified_at < :at
```

- `phase` and `updated_at`: an observation of an old row does not count.
- `notified != class`: two processes that see the same change write once.
  Only the process whose update changed the row sends.
- `notified_at < at`: an old observation does not overwrite a newer one.
- A phase event sends only when `notified_phase` is not the row's
  `updated_at`. Thus `ready → needs input → ready` (a held message) sends
  `ready` once.

The migration fills the columns for existing tasks (`ready`, `needs_you`,
`failed` phases, and `done` for running tasks without a flow), so an
update does not send old events again.

## Mechanism per OS

csquad selects one backend at run time.

| OS              | Backend                                                       |
|-----------------|---------------------------------------------------------------|
| Windows         | WinRT toast through a hidden `powershell.exe` (Windows PowerShell 5.1) with the app ID `csquad` |
| WSL             | the same toast, through `powershell.exe` (interop)            |
| macOS           | `UNUserNotificationCenter` in `~/.csquad/csquad.app`, called through purego (no cgo) |
| Linux desktop   | D-Bus `org.freedesktop.Notifications.Notify` on the session bus (godbus, pure Go) |
| Linux, no bus   | none                                                          |

### Windows and WSL

- `install` registers the app ID per user, no admin rights:
  `HKCU\Software\Classes\AppUserModelId\csquad` with `DisplayName` =
  `csquad` and `IconUri` = the icon file.
- Windows: csquad writes the icon to `%CSQUAD_HOME%\csquad.png` and writes
  the key with the registry API.
- WSL: the toast cannot read a file in the Linux file system. csquad
  writes the icon to `%LOCALAPPDATA%\csquad\csquad.png` and the key
  through one `powershell.exe` script.
- The toast: `ToastNotificationManager.CreateToastNotifier("csquad")`, a
  `ToastGeneric` toast with two text lines. The script goes in
  `-EncodedCommand`, so no quoting problem occurs. PowerShell 7 cannot load
  WinRT types, so csquad uses Windows PowerShell.
- WSL detection: `WSL_DISTRO_NAME`, or `microsoft` in `/proc/version`.

### macOS

- Only an app bundle can use `UNUserNotificationCenter`. `install` writes
  `~/.csquad/csquad.app`:
  - `Contents/Info.plist`: `CFBundleIdentifier` =
    `io.github.shunl12324.csquad`, `CFBundleName` = `csquad`,
    `LSUIElement` = true.
  - `Contents/Resources/csquad.icns`: the icon (PNG data in an icns file).
  - `Contents/MacOS/csquad`: a copy of the binary.
- `install` signs the bundle ad hoc (`codesign -s - --force`).
- To send, csquad runs the binary inside the bundle (`notify post`), so the
  notification shows as `csquad`.
- The first notification asks the user for permission once.
- Fallback, if the bundle cannot post: `osascript -e 'display
  notification'`. It shows as Script Editor.

### Linux

- `Notify` on the session bus (`DBUS_SESSION_BUS_ADDRESS`, else
  `$XDG_RUNTIME_DIR/bus`), app name `csquad`, the icon from
  `~/.csquad/csquad.png`.
- No session bus: no notification and no error.

## Icon

The pixel `>_C` logo of `docs/assets/banner.svg`, mint `#78e8af` on
`#1e1e2e`, on a square. csquad draws it in Go (no binary file in the
repository) and writes it at install: PNG for Windows and Linux, icns for
macOS.

## Configuration

- `CSQUAD_NOTIFY=off`: no notifications.
- `CSQUAD_NOTIFY=input`: only `needs input` and `needs you`.
- `CSQUAD_NOTIFY=on` (default): all events.
- `csquad notify on|input|off` keeps the setting in
  `$CSQUAD_HOME/notify`, so the status line, the hooks and the detached
  processes use the same setting. The variable wins over the file.
- `csquad notify test`: sends a test notification. It prints the backend,
  or why there is none.
- `CSQUAD_NOTIFY_FILE=PATH` (for tests and debugging): append each
  notification as a JSON line to PATH, and show nothing.

## Install and uninstall

- `install` / `update`: write the icon and the bundle (macOS), register the
  app ID (Windows, WSL), and print the backend, for example
  `Notifications: Windows toast (app ID csquad)`.
- The manifest records what install made (`notify` block: backend, icon
  files, bundle, registry key, icon path in the key).
- `uninstall` removes the icon, the bundle, and the registry key. It
  removes the key only if `IconUri` still names this installation's icon:
  a native Windows install and a WSL install can share the key.
- An install step for notifications that fails prints a warning. It does
  not stop the install.

## Failure behaviour

- A notification failure never fails a command, a hook or the status line.
- The detached sender has a 20 s timeout. It writes errors to
  `$CSQUAD_HOME/logs/notify.log`.
- If no backend is available, csquad sends nothing and logs nothing.

## Out of scope (v1)

- Click actions (open the session, attach, finish).
- Sounds, grouping, and replacing old notifications.
- Notifications for tasks of other sessions when no csquad process runs.
- A notification centre on Linux without D-Bus.
