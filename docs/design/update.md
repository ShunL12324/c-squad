# Design: team version pinning and `csquad update`

Status: design for review (T224). Nothing here is implemented yet.

## Goal

Installing a new csquad must not change a running team, the way a Claude Code
update only takes effect the next time Claude Code starts. A team keeps using
the csquad it started with until it is resumed. `csquad update` upgrades
through whichever package manager installed csquad, and never replaces files
that package manager owns.

Package managers replace their files in place, so isolation cannot come from
them. csquad provides it by giving each team its own copy of the binary.

## 1. Pinned copies

### Store

- Location: `${XDG_DATA_HOME:-$HOME/.local/share}/csquad/versions/`, or
  `CSQUAD_VERSIONS_DIR` when set. The override is an absolute path, checked
  the same way. It exists for home directories mounted `noexec`.
- Each entry is `<version>-<sha256 hex>/csquad`, using the full hash, so two
  builds with the same version string never collide.
- The `csquad` and `versions` directories are created 0700. Before every use,
  csquad checks with `Lstat` that each is a real directory (not a symlink),
  owned by the current uid and not group- or world-writable. Otherwise it
  refuses with the path and the reason.
- A pinned file is a regular file, mode 0500, owned by the uid, never a symlink
  or hardlink. It is an independent copy, so its inode is unrelated to the
  installed file.

### Creating a pin

1. Open the running image. On Linux this is `/proc/self/exe`, which is still
   the running inode even after a package manager has replaced or deleted the
   path. On macOS it is `os.Executable()` resolved with `EvalSymlinks`.
2. Stream it into `O_CREAT|O_EXCL` temp file in the version directory,
   hashing while copying. Then fsync, chmod 0500, and fsync again.
3. Verify the copy is this build: run `<temp> version` and compare its output
   (version, commit and build date) with the running build's own
   `buildinfo.String()`. On macOS the file at the
   path may already be newer than the running image; a mismatch fails the pin
   with "csquad was replaced while running; run the command again". If the
   exec itself fails with `EACCES` or `ENOEXEC` (a `noexec` mount), the pin
   fails with "cannot execute files in DIR (noexec?); set
   CSQUAD_VERSIONS_DIR to an executable directory".
4. `rename` the temp file into `<version>-<sha>/csquad`, then fsync the
   directory.
5. If the destination already exists, hash it. If the hash matches, reuse it:
   pinning is idempotent and safe to run concurrently. If it differs, the entry
   is corrupt; atomically rename the verified copy over it, since the name is
   content-addressed and the new bytes are the correct ones.

### Recording the pin

There is no new ledger field. The pin is `State.Executable`, a field every
released version already reads and writes back unchanged. A team is **pinned**
when `Executable` is exactly `<store>/versions/<version>-<sha256>/csquad`. The
version and the full expected hash are parsed from that content-addressed
path, so an older binary rewriting the ledger cannot drop the pin record. Any
other `Executable` (every team created by ≤0.11.x) is **unpinned**.

Pinning happens only at these points:

- On `new`/`start`, before the first member launches.
- On `resume`, after the old processes are reaped and while
  `team-lifecycle` is held, in the same `st.update` that already rewrites
  `Executable` (`recovery.go:297-320`). This is the only point where a team
  moves to another version.
- On `csquad repin TEAM`, the explicit recovery described below.

The pin never changes while a team is active. `recover`, `member restart` and
`member replace` keep it.

**No downgrade by accident.** `resume` and `repin` pin the *current* binary.
Several installs can coexist (nvm npm, Homebrew, a stray old file on PATH), so
a re-pin could otherwise silently downgrade a team and drop newer ledger
fields. Before any change, and for `repin` before it stops anything, the
current build is compared with the pinned one. There is no override flag.

| Current vs pinned | Decision |
|---|---|
| Same hash | Not a version change; allowed. |
| Both stable `X.Y.Z`, current newer (numeric semver order, never string order) | Allowed: this is the upgrade. |
| Both stable `X.Y.Z`, current older | Refused: "team T is pinned to csquad A; this is B, which is older. Use csquad A: PINNED-PATH resume T". |
| Anything not comparable: either side not a stable `X.Y.Z` (`dev`, prerelease, `unknown`), or the same version with a different hash | Order unknown, so safety cannot be decided. On an interactive TTY csquad names both builds and hashes and asks `Pin team T to this build? [y/N]`; the default is no. Without a TTY it is refused. |

The recovery contract is always the same: the team's own pinned build still
exists in the store and can resume it (`PINNED-PATH resume T`), so a refusal
never strands a team. Known limitation: a local build and a release that share
a version string cannot be ordered; csquad asks, it does not guess.

**Every embedded path is regenerated after a re-pin.** A re-pin happens only
while the team is stopped: `resume` and `repin`, after stopping. Stopping ends
the runtime session and every member session. `resume` then creates a new tmux
socket and new generations, and relaunches everything from the new
`State.Executable`:

| Entry point | Source |
|---|---|
| runtime session | `pump.go` |
| member launch and run-engine argv | `engine.go:170` |
| engine hook command, passed at each launch | `engine.go:79` |
| member PATH wrapper, in a per-generation directory | `member_environment.go` |
| tmux navigation and panel bindings, on the new server | `configureNavigation`, `navigation.go`, `panels.go` |
| master exit and shutdown `run-shell` | `recovery.go:78` |
| hook-triggered sync | `hooks.go:121`, through the wrapper and hooks |

If an entry point were ever missed, the old pinned file still exists. With the
self check (Verification below), a writer running from a path other than
`State.Executable` is refused, so a stale binding fails loudly instead of
writing as a second version.

### Verification

Verification is enforced at the write chokepoint, not only on forwarding.
Every ledger write goes through `Store.update`, and every `update` transaction
runs a **self check** against the `State.Executable` it has just read inside
that same transaction:

- The process's **identity** is computed once and cached: its resolved own
  path and the SHA-256 of its running image. The image is read from
  `/proc/self/exe` on Linux, or on macOS from the path it was started from,
  after checking owner, mode and that it is a regular file and not a symlink.
- The **comparison** runs in every transaction. For a pinned team, the cached
  path must equal the transaction's `State.Executable`, and the cached hash
  must equal the hash in that path.

A long-lived process therefore cannot keep writing after the pin changes. A
runtime, runner or panel left from before a re-pin fails its next write.

On failure the transaction is aborted with the recovery hint and the ledger is
untouched.

This covers every writer the pinned file runs as: the member PATH wrapper
(each agent `csquad` call), hooks, run-engine, runtime, navigation and panel
keys, shutdown and hook-triggered sync. It also covers a current-version
writer that was not forwarded, such as a code path missed in §2, which is
refused instead of silently mixing versions.

**The pin transition.** Changing the pin is the only write that may run while
the process is not yet the pin. This is not a command-wide bypass:

- `start` creates the ledger with the pin in its first transaction, so the
  creator is the pin from the first write.
- `resume` and `repin` hold `team-lifecycle`, confirm the team is inactive
  (`repin` first stops an active team, see §3), and reap the old processes.
  Everything up to that point only reads the ledger, and the implementation
  moves any write it finds there to after the transition. Then a single
  **transition transaction** sets `Executable` to the new pin. It is marked by
  an in-process token that only this code path creates, and it is the only
  transaction the self check lets through with a mismatched pin. Every later
  write in the same command runs as the new pin and passes normally.
- The one mixed write is `repin` stopping an **active** team whose pin is
  broken. The user confirms it, and it is limited to the stop transaction.

Unpinned teams keep today's behaviour, with no self check.

Before csquad *starts* a process from the pinned path (`startRuntime`, member
launch, `recover`) and before forwarding (§2), it verifies the target file the
same way: owner, mode, regular file and hash. A broken pin therefore fails
before anything is launched.

Hashing a roughly 10 MB binary takes a few milliseconds, once per process.
Implementation measures it on an agent CLI call; if the cost matters, the
check is kept and the reason recorded. The member PATH wrapper also checks
`[ -x "$pinned" ]`, only to print the recovery hint instead of a shell "not
found".

Trust boundary: the store protects against accidental change: replacement,
deletion, partial copy, a corrupt entry or a wrong version. It does not
protect against the same user deliberately editing the file, since that user
could equally edit the ledger.

**Only the pinned binary drives a pinned team's runtime.** `startRuntime`
kills and relaunches the runtime when the protocol differs from the caller's
own constant. These are tmux and process operations, not ledger writes, so the
`Store.update` check does not cover them. `startRuntime` and `syncMessages`
therefore compare the caller's identity with the pin first, and on a pinned
team they return without effect when the caller is not the pin. Otherwise an
unforwarded newer binary would restart the pinned runtime on every call, a
restart loop. The same guard applies to every tmux kill of team sessions
outside the transition.

Exempt commands never reach them: `list`, `version`, `doctor`, `config`,
`completion`, `usage` and `update` only read ledgers. Their read path is
`st.read`, which normalizes an in-memory copy and never persists. The
implementation keeps that true and adds a test that these commands leave a
pinned team's ledger bytes and tmux sessions unchanged. The exceptions are
`resume` and `repin`, which re-pin first and only then start or stop anything.
The implementation audits every call site, and a test checks it.

## 2. One writer version per team

Writers **inside** a team all go through `State.Executable`: the member PATH
wrapper, hooks, run-engine, runtime, navigation and panel keys, shutdown, and
hook-triggered sync. Once pinned, they all run the pinned binary.

Entry points **outside** a team run whatever `csquad` is on PATH, for example
`attach`, `stop`, `board` or `task …` from a human terminal. The new binary
forwards these:

- **Where:** in `Execute`, after the team is fully resolved exactly as the
  command itself resolves it, then a read-only `st.read()`, before anything
  writes. Resolution follows the existing order: positional `TEAM` for
  `attach`, `stop`, `recover` and `ui`, the legacy `attach MEMBER` rule,
  `--name`, `--team-name`, `--team`/`--state-dir`, a bound
  `CSQUAD_STATE_DIR`, and the current project's last team. The original argv
  is forwarded unchanged, so the pinned binary resolves the same team again.
- **Condition:** the team is pinned, the pinned path differs from the resolved
  own executable, and the command is not in the exemption list.
- **Action:** verify the pinned file (§1), then `syscall.Exec` it with the
  same argv and environment, and the same cwd. The exit code is the pinned
  binary's own.
- **Loop guard:** `CSQUAD_FORWARDED=<pinned sha>` is set for the exec. A
  process that sees this marker and would forward again fails with
  "forwarding loop". It also fails when the marker does not match its own
  hash.
- **Read-only commands forward too** (`board`, `task inspect`, `member list`,
  `ui`…). This keeps one rule, and keeps output and ledger interpretation
  consistent with the team's version.
- **Exempt, run by the current binary:** `new`/`start` (they create a new
  team), `list`, `version`, `doctor`, `config`, `completion`, `usage`/help,
  `update`, `resume` (it re-pins) and `repin`.
- **`team remove`:** forwarded when the pin is intact, so the pinned version
  removes its own state. When the pin is broken it is refused with the
  `repin` hint.
- **Notice:** when the pinned version differs from the current one and stderr
  is a terminal, one line is printed first: `csquad: team X runs csquad
  0.12.0 (pinned); resume it to move to 0.13.0`.

## 3. Missing or corrupt pin

There is no silent fallback. When verification fails, csquad fails with the
team, the recorded version and hash, the path and what was wrong.

One case is not a fallback: if the current binary's hash equals the recorded
hash, csquad re-creates the file from itself (§1) and continues. The bytes are
identical, so no other version writes.

Otherwise, the error names one explicit recovery command:

```
csquad repin TEAM
```

`repin` is human-only (refused in member sessions) and not forwarded. It
refuses when the pin verifies ("pin is intact; resume to upgrade").

- For an inactive team, it pins the current binary.
- For an active team, it asks for confirmation (`--yes` for scripts). It then
  stops the team with the current binary, which is an explicit, acknowledged
  one-off mixed write, and pins the current binary. It prints `csquad resume
  TEAM` as the next step.

## 4. First-upgrade boundary

Released ≤0.11.x has no pinning and no forwarding. It cannot be changed
retroactively.

- An **active team started by ≤0.11.x** becomes mixed the moment the package
  manager replaces the file. The old runtime and runners keep the old inode,
  and new hooks and member CLI calls run the new binary. The old bytes are
  gone, so the new binary cannot pin that team.
- On every command against an active unpinned team, if stderr is a TTY, the
  new binary prints: `team X is not pinned to a csquad version; stop and
  resume it to pin`. Nothing is recorded, so the notice itself never writes
  the ledger. It then works as today; it does not refuse, because refusing
  would strand a running team with no way to stop it.
- **Every install must be upgraded together.** A ≤0.11 csquad left anywhere
  on PATH (another npm prefix, Homebrew, a stray copy) can still write any team
  without forwarding. `doctor` warns when more than one `csquad` executable is
  on PATH and names their versions.
- **Recommended path, documented in the release notes and `docs/install.md`:**
  before the first upgrade to the pinning release, run `csquad stop` for each
  active team, then upgrade, then `csquad resume`. From then on, upgrades never
  touch running teams.
- **Later updates:** `csquad update` (§5) lists the teams it can find (the
  same discovery as `csquad list`). If any is active and unpinned, it warns and
  asks for confirmation.

## 5. `csquad update [--check] [--yes]`

### Refusal and flags

`update` is refused inside a member session: when `CSQUAD_MEMBER_ID` or
`CSQUAD_STATE_DIR` is set, csquad names the variable and says to run it from a
terminal outside the team. `update` never operates on a team and is not
forwarded.

- `--check` reports the installed version, the channel and the latest release,
  and installs nothing. It queries
  `https://api.github.com/repos/ShunL12324/c-squad/releases/latest` with a
  10-second timeout. A network failure or GitHub's unauthenticated limit of
  60 requests per hour is reported as "unable to query the latest release:
  REASON".
- The npm, Homebrew and APT updates do not depend on the GitHub API: the
  package manager decides what is newest. Only `--check` and the local `.deb`
  path query GitHub.
- Without `--check`, `update` prints the exact commands it will run (argv, no
  shell) and asks `Proceed? [y/N]`. `--yes` skips the question.
- Without a TTY and without `--yes`, it prints the commands and exits 1.

### Channel detection

Detection works from the resolved real path `R` of the running binary
(`/proc/self/exe` or `EvalSymlinks(os.Executable())`) and is cross-checked with
the package manager's own metadata.

| Channel | Evidence | Command | Needs root |
|---|---|---|---|
| npm | `R` = `P/lib/node_modules/csquad/native/<os>-<arch>/csquad`, and `P/lib/node_modules/csquad/package.json` has name `csquad` | `NPM install --global --prefix P csquad@latest`; `NPM` is `P/bin/npm` if executable, else `npm` on PATH | when `P/lib/node_modules` is not writable |
| pnpm / yarn / bun global | `R` under `node_modules/csquad/` with a pnpm, `.bun` or yarn global layout | hint only: `pnpm add -g csquad@latest` or the matching command | — |
| Homebrew | `R` = `C/csquad/<kegver>/bin/csquad`, `B = dirname(C)/bin/brew` is executable, and `B --cellar` resolves to `C`; this works for any prefix | `B upgrade <tap>/csquad`; `<tap>` comes from the keg's `INSTALL_RECEIPT.json` `source.tap` (compared case-insensitively; receipts store lowercase names such as `shunl12324/c-squad`), falling back to `B list --full-name --formula` | refused if `C/csquad` is not writable (Homebrew refuses root) |
| APT | `dpkg-query -S R` reports package `csquad`, and `apt-cache policy csquad` lists an origin under `shunl12324.github.io/c-squad/apt` | `sudo apt-get update`, then `sudo apt-get install --only-upgrade csquad`; the confirmation text says that `apt-get update` refreshes every configured source | yes |
| Local `.deb` | owned by the `csquad` package, no csquad APT origin | download and verify the release `.deb` (below), then `sudo apt-get install <tmpdir>/csquad_<ver>_<arch>.deb` | yes |
| Manual / source | none of the above | hint only: latest version, `docs/install.md` link | — |

Notes on the npm row:

- `--prefix P` targets the exact installation that owns `R`, whichever node
  version or nvm directory the `npm` on PATH belongs to.
- Global npm packages are plain files, so any npm works.
- When root is needed, csquad prints `sudo …` instead of running it. `sudo`
  resets `PATH`, so `#!/usr/bin/env node` may not find nvm's node, and npm
  itself discourages sudo.

### Local `.deb` download

The user installed a `.deb` from GitHub Releases, so `update` follows the same
source the installation docs point to.

1. Resolve the release from the `releases/latest` API response: `tag_name`
   must be a stable `vX.Y.Z`, newer than the installed version. The
   architecture comes from `dpkg --print-architecture` and must be `amd64` or
   `arm64`.
2. Choose exactly two assets from that response by name:
   `csquad_<X.Y.Z>_<arch>.deb` (goreleaser's `ConventionalFileName`) and
   `checksums.txt`. Download them by their `browser_download_url`, over HTTPS
   only, to a fresh 0700 temp directory.
   - Size caps: 64 MiB for the `.deb`, 1 MiB for the checksums. Downloads
     stop at the cap.
   - Timeouts: 30 s to connect and read headers, 5 minutes overall.
   - Redirects are followed only to `https` hosts.
3. Verify:
   - the SHA-256 of the `.deb` equals its line in `checksums.txt`;
   - `dpkg-deb --field <file> Package Version Architecture` reports `csquad`,
     `X.Y.Z` and `<arch>`.

   Any mismatch deletes the files and aborts before sudo.
4. Show the verified version, hash and the exact `sudo apt-get install
   <abs path>` command, confirm, run it on an interactive TTY only, then
   delete the temp directory.

Trust boundary: GitHub's HTTPS and the release's `checksums.txt`, the same
evidence a manual download relies on. The checksums file is not separately
signed, so it protects against a truncated or wrong download but not against a
compromised release. The docs say so and recommend the APT source, which is
signed, for signature verification.

### Running the command

- Commands run with argv only, and stdin, stdout and stderr connected to the
  terminal.
- `sudo` runs only for APT, and only on an interactive TTY after
  confirmation.
- A non-zero exit is reported with the command and its code; nothing is retried.

### Afterwards

The upgrade may delete `R`: `brew upgrade` removes the old keg by default. So
`update` re-resolves the channel's stable entry and runs `<entry> version`:

| Channel | Entry |
|---|---|
| npm | `P/lib/node_modules/csquad/native/<os>-<arch>/csquad` |
| Homebrew | `dirname(C)/opt/csquad/bin/csquad`; the opt link follows the linked keg, which is confirmed with `B --prefix csquad` |
| APT and local `.deb` | the file `dpkg-query -L csquad` lists under `/usr/bin` |

If the entry is missing or still reports the old version, `update` reports
that instead of claiming success.
- It lists the teams it found with their pinned versions:
  `team X: csquad 0.12.0 (pinned) — resume to move to 0.13.0`.

### Out of scope

- No daily update checks and no background updates.
- No automatic garbage collection, because teams in other projects cannot be
  discovered completely. Old `versions/` entries stay until removed by hand;
  the docs say so, and `update --check` prints the directory's size.

## 6. Compatibility and the downgrade boundary

- No new ledger field, and `stateVersion` stays 3. The pin lives in
  `Executable`, which older versions preserve.
- **Pinning protects a team only against a newer csquad.** It does not protect
  against an older one.
- **Running an older csquad against a pinned team is not safe and not
  supported.** Versions before pinning neither forward nor run the self check,
  so an older binary on PATH writes the ledger directly as a second writer
  version.
  - It drops fields it does not know, for example a `task cancel` record
    (reason, actor, time).
  - It may act on state it does not understand.
  - If it resumes the team, it re-points `Executable` at itself, which
    unpins the team.

  csquad cannot prevent this. The docs say: do not use an older csquad on a
  team pinned by a newer one; to go back, stop the team and resume it with the
  older version, accepting those losses.
- runtimeProtocol is bumped, so a runtime from an older version is replaced on
  upgrade.
- The member PATH wrapper script gains only the `-x` check.

## 7. Verification plan

Everything uses temp directories and fake `npm`, `brew`, `dpkg-query`,
`apt-cache`, `apt-get` and `sudo` executables on PATH. Nothing installs or
upgrades anything real.

Pinning:

- The copy is not a hardlink (different inode, `Nlink == 1`), has mode 0500,
  and has the right owner and directory modes.
- Hardening: an insecure or symlinked directory is refused.
- Idempotent, and concurrent pins of the same build (goroutines plus
  processes) leave one valid file.
- A corrupt existing entry is replaced.
- A version mismatch when the source changed mid-pin is refused.

Isolation:

- Start a team with a built binary, then overwrite the source file in place
  and delete it. The runtime, a member launch, recover and hooks still run the
  pinned build (reported by `version`).
- A new `csquad` on PATH forwards `board`, `task progress` and `stop` to the
  pinned build.
- The loop guard triggers.
- The exemption list is not forwarded.

Missing or corrupt pin:

- Forwarding, launch and runtime start fail with the recovery hint and write
  nothing (ledger bytes unchanged).
- Self check: a pinned team written by a binary at another path, or by an image
  whose hash differs (a test build with a different embedded marker), is
  refused before the ledger changes. This covers a hook, a wrapper call and an
  unforwarded CLI path.
- The identical-hash self-heal works.
- `repin` works on an inactive team and on an active team with `--yes`, and
  is refused when the pin is intact and in member sessions.

Re-pin, downgrade and runtime:

- After a re-pin, the old pinned path appears nowhere: `tmux list-keys`, the
  generated hook command, the wrapper, the runtime and member session
  commands, and the shutdown hook.
- A binding left on the old pin is refused by the self check.
- Version decision table:
  - stable newer is allowed;
  - stable older is refused, with the pinned-path hint;
  - same hash is allowed;
  - `dev`, prerelease or same version with a different hash asks on a TTY
    (default no) and is refused without a TTY;
  - semver ordering is numeric (`0.10.0 > 0.9.0`).
- Self check in every transaction: a process whose first write passed is
  refused after a re-pin changes `State.Executable`.
- The transition token is honoured only inside resume/repin after reaping; a
  forged mismatch outside it is refused.
- An exempt newer binary does not restart the pinned runtime; the runtime PID
  is unchanged across calls.
- `noexec` store: a fake exec failure gives the clear error, and
  `CSQUAD_VERSIONS_DIR` is honoured.
- Forwarding under each resolution path: positional `TEAM`, `--team-name`,
  `--team`/`--state-dir`, bound `CSQUAD_STATE_DIR`, the cwd project, and the
  legacy `attach MEMBER`.
- `doctor` reports multiple csquad executables on PATH.

Resume:

- It re-pins only after the old processes are reaped, under
  `team-lifecycle`.
- Concurrent `resume`, `resume` and `attach` do not leave a runtime on the old
  pin.

Old teams:

- An unpinned ledger loads, prints the one warning and works as before.
- Resume pins it.

`update`:

- For each channel: the exact argv, prefix handling (nvm-like, `/usr`,
  user prefix, a Homebrew prefix in an arbitrary temp dir with
  `--cellar` confirmed), and tap from the receipt and from the fallback.
- Root rules: npm prints `sudo`, and Homebrew refuses a non-writable Cellar.
- TTY and `--yes` rules.
- Refused inside a member session.
- Local `.deb`: a fake HTTPS release server (`httptest` TLS) serves the API
  response, the `.deb` and checksums; a fake `dpkg-deb` reports the fields.
  Covered: checksum mismatch, field or version mismatch, oversize, timeout,
  wrong architecture, not newer, non-TTY and non-confirmation. The files are
  removed on every path.
- Post-update entry resolution: the Homebrew old keg is deleted by the fake
  `brew`, and the new version is read from `opt`.
- `--check` runs no install (the fakes record calls).
- Failure paths: network error, fake exits non-zero, unknown channel hints.

Finally, `make check` and the applicable packaging tests.
