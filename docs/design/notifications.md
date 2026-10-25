# Native notifications

Status: implemented for 3.1.0. The section "Changes from the proposal"
lists what the implementation changed, and why.

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
| `failed`      | the session of a task without a flow failed, or the phase is `failed` | on |
| `merged`      | `--merge` landed the work (phase `done` after a landing)    | on      |

csquad does not send a notification while a task works, for usage changes,
or for what the user did (`finish`, `cancel`, `retry`).

- A task with a check or a merge is idle between the steps of its flow.
  That is not `done`: its end is `ready` or `merged`.
- When the session of a task with a flow fails, the flow moves the task to
  the phase `failed`. That move sends `failed`, once.
- `done` needs a final message. An idle session that has not answered yet
  (right after its start) sends nothing.

## Content

- Title: `T2 · fix login redirect`.
- Body: the state, then one detail:
  - `needs input`: the first line of the last assistant message.
  - `needs you`: the reason (the note).
  - `done`: the first line of the final message.
  - `merged`: the target branch (`merged · into main`).
- The first line loses markdown heading, quote, list and bold marks.
- The title is at most 80 characters, the body at most 160. They never hold
  settings, tokens or the prompt.

## Where it fires

All of csquad's short processes observe tasks: the status line, the Stop
hooks (through the detached `advance`), `wait`, `check`, and the
`AdvanceAll` of each command. Two places record a state:

1. `App.observe`: each engine observation of a task's session. The view
   (`view(t, live)`) gives the event class (`noticeClass`).
2. A phase change that csquad writes (`flow.Mover.Apply` through its
   `Moved` hook, `App.fail`): `ready`, `needs you`, `failed`, `merged`.

Both places call one function, `App.notice`. When the class is the one
the row already records, it does nothing (no write). Otherwise it claims
the event in the database and, if the claim wins, starts a detached
`csquad notify send T12 EVENT`. The detached process reads the task, makes
the text, and shows the notification. The status line writes at most one
row and starts at most one process per change, so it stays in its 3.5 s
budget.

The Stop hook also starts `advance --stopped` for a task without a check
or a merge (unless notifications are off), so `done` comes also when no
status line runs (a Codex master).

## Deduplication

Migration 3 adds three columns to `tasks`:

- `notified`: the last observed event class (`""` for a quiet state such
  as working).
- `notified_at`: the time of that observation (unix ms).
- `notified_phase`: the `updated_at` of the row that the last phase event
  (`ready`, `needs you`, phase `failed`, `merged`) was for.

The claim (`store.ClaimNotice`) is one compare-and-set in an immediate
transaction:

- The row must still have the observed `phase` and `updated_at`: an
  observation of an old row does not count.
- `notified_at` must be older than the observation: an old observation
  does not overwrite a newer one.
- A live event (`needs input`, `done`, `failed` of a task without a flow)
  is sent when `notified` changes. Two processes that see the same change
  write once; only the process whose update changed the row sends.
- A phase event is sent once per version of the row: when
  `notified_phase` is not the row's `updated_at`. Thus `ready → needs
  input → ready` (a held message) sends `ready` once, and `needs you →
  fixing → needs you` sends `needs you` twice, also when no process saw
  the quiet phase between.

The migration fills the columns for existing tasks (`ready`, `needs_you`,
`failed` phases, and `done` for running tasks without a flow), so an
update does not send old events again.

## Mechanism per OS

csquad selects one backend at run time.

| OS              | Backend                                                       |
|-----------------|---------------------------------------------------------------|
| Windows         | WinRT toast through a hidden `powershell.exe` (Windows PowerShell 5.1) with the app ID `csquad` |
| WSL             | the same toast, through `powershell.exe` (interop)            |
| macOS           | `UNUserNotificationCenter` in `~/.csquad/csquad.app`, called through purego (no cgo); without the bundle, `osascript` |
| Linux desktop   | D-Bus `org.freedesktop.Notifications.Notify` on the session bus (godbus, pure Go) |
| Linux, no bus   | none                                                          |

### Windows and WSL

- `install` registers the app ID per user, no admin rights:
  `HKCU\Software\Classes\AppUserModelId\csquad` with `DisplayName` =
  `csquad` and `IconUri` = `%LOCALAPPDATA%\csquad\csquad.png`.
- Windows: csquad writes the icon and the key with the registry API
  (`golang.org/x/sys/windows/registry`).
- WSL: csquad writes the icon and the key through one `powershell.exe`
  script. A toast cannot read a file in the Linux file system.
- The icon has one path for a native and a WSL installation. Windows
  caches the icon path of an app ID: after a change of `IconUri`, it still
  reads the first path. A toast after the change shows no icon. Windows
  reads the file at that path when it shows a toast, so a new icon goes
  into the same file. A versioned file name would lose the icon.
- The toast script first repairs the app ID when the key is missing, the
  icon file is missing, or `IconUri` names another file. Thus a native and
  a WSL installation can share the app ID, and the uninstall of one does
  not break the other.
- The toast: `ToastNotificationManager.CreateToastNotifier("csquad")`, a
  `ToastGeneric` toast with two text lines. The script goes in
  `-EncodedCommand` and the XML goes in as base64, so no quoting problem
  occurs. PowerShell 7 cannot load WinRT types, so csquad uses Windows
  PowerShell.
- Each script starts with `$ProgressPreference = 'SilentlyContinue'` and
  UTF-8 output. csquad reads stdout and stderr apart: with redirected
  output, PowerShell writes progress and errors to stderr as CLIXML.
- WSL detection: `WSL_DISTRO_NAME`, or `microsoft` in `/proc/version`.

### macOS

- Only an app bundle can use `UNUserNotificationCenter`. `install` writes
  `~/.csquad/csquad.app`:
  - `Contents/Info.plist`: `CFBundleIdentifier` =
    `io.github.shunl12324.csquad`, `CFBundleName` = `csquad`,
    `LSUIElement` = true.
  - `Contents/Resources/csquad.icns`: the icon (PNG data in an icns file:
    `ic07`, `ic08`, `ic09`).
  - `Contents/MacOS/csquad`: a copy of the binary. A symlink cannot be the
    main executable of a signed bundle.
- `install` signs the bundle ad hoc (`codesign --force --sign -`) and
  registers it with LaunchServices (`lsregister -f`). The notification
  service asks LaunchServices for the app. LaunchServices does not accept
  a bundle below `/tmp`, so `CSQUAD_HOME` must not be there.
- To send, csquad runs the binary inside the bundle (`notify post`), so the
  notification shows as `csquad` with its icon.
- Permission: macOS asks once. It removes the question when the asking
  process ends, and an unanswered question then counts as declined. So
  `post` reads the status first:
  - not determined: it asks and keeps running up to 5 minutes for the
    answer;
  - denied: it fails at once and names System Settings › Notifications;
  - allowed: it posts.
- Fallback, when there is no bundle (no install): `osascript` with
  `display notification`. It shows as Script Editor.

### Linux

- `Notify` on the session bus (`DBUS_SESSION_BUS_ADDRESS`, else
  `$XDG_RUNTIME_DIR/bus`), app name `csquad`, the icon from
  `~/.csquad/csquad.png`. csquad never starts a bus.
- No session bus: no notification and no error.

The detached processes get `DBUS_SESSION_BUS_ADDRESS`, `WSL_DISTRO_NAME`
and `WSL_INTEROP` (added to the allowlist of `proc.Env.Base`).

## Icon

The pixel `>_C` logo of `docs/assets/banner.svg`, mint `#78e8af` on
`#1e1e2e`, on a square with round corners. csquad draws it in Go (no
binary file in the repository) and writes it at install: PNG for Windows
and Linux, icns for macOS.

## Configuration

- `CSQUAD_NOTIFY=off`: no notifications.
- `CSQUAD_NOTIFY=input`: only `needs input` and `needs you`.
- `CSQUAD_NOTIFY=on` (default): all events.
- `csquad notify on|input|off` keeps the setting in
  `$CSQUAD_HOME/notify`, so the status line, the hooks and the detached
  processes use the same setting. The variable wins over the file.
- `csquad notify`: shows the setting and the backend.
- `csquad notify test`: sends a test notification. It prints the backend,
  or why there is none. On Windows it also shows the app ID's state.
- `CSQUAD_NOTIFY_FILE=PATH` (for tests and debugging): append each
  notification as a JSON line to PATH, and show nothing. It is sent at
  once, without a detached process. `install` then sets nothing up.

## Install and uninstall

- `install` / `update`: write the icon, the bundle (macOS), register the
  app ID (Windows, WSL), and print the backend, for example
  `Notifications: Windows toast (app ID csquad)`.
- The manifest records what install made, in the block `notify`:
  `backend`, `files` (in `$CSQUAD_HOME`), `appId` (`key`, `icon`) and
  `wsl`. An update removes the files an older install made that it no
  longer makes.
- `uninstall` removes the files, the bundle (and its LaunchServices
  entry), and the app ID and its icon. It removes the key only if
  `IconUri` still names the icon it registered. It never removes a file
  outside `$CSQUAD_HOME`, except the app ID's icon. It keeps the setting
  `$CSQUAD_HOME/notify`.
- An install step for notifications that fails prints a warning. It does
  not stop the install.

## Failure behaviour

- A notification failure never fails a command, a hook or the status line.
- The detached sender has a 20 s timeout (`CSQUAD_NOTIFY_TIMEOUT_MS`),
  and 5 min 20 s on macOS, where the first notification can wait for the
  user's permission. It writes errors to `$CSQUAD_HOME/logs/notify.log`.
- If no backend is available, csquad sends nothing and logs nothing.

## Changes from the proposal

- The Windows icon moved from `%CSQUAD_HOME%\csquad.png` to
  `%LOCALAPPDATA%\csquad\csquad.png`, and the toast repairs the app ID.
  Reason: the icon cache of Windows (see "Windows and WSL"). In the real
  check, toasts after a change of `IconUri` showed no icon.
- PowerShell output: progress off, stdout and stderr apart. Reason: CLIXML
  progress records went into the recorded icon path.
- macOS permission: `post` keeps the question up to 5 minutes and fails at
  once when denied. Reason: in the real check, the question went away
  with the process and the app then counted as declined.
- `CSQUAD_NOTIFY_FILE` sends without a detached process. Reason: tests
  must not leave a process that writes into a removed directory.

## Out of scope (v1)

- Click actions (open the session, attach, finish).
- Sounds, grouping, and replacing old notifications.
- Notifications for tasks of other sessions when no csquad process runs.
- A notification centre on Linux without D-Bus.
