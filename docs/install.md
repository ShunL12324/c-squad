# Installation

Install a precompiled package with npm, Homebrew, APT, or a download from
[GitHub Releases](https://github.com/ShunL12324/c-squad/releases/latest).
You do not need Go or a compiler.

## npm

```sh
npm install -g csquad
csquad doctor
csquad new -s my-team
```

Create a named team with `csquad new -s NAME`; `csquad start --name NAME`
remains compatible. Use `list` to find teams, `attach NAME` to enter a running
team, `resume NAME` to restore a stopped team, and `stop NAME` to stop it.

Or run `npx csquad`. Install npm inside WSL 2 when using Windows.
The package contains native binaries for macOS and Linux on x64 and arm64;
installation needs no Go compiler, lifecycle scripts, or GitHub downloads.

npm does not manage system packages. Install tmux, ps, and Git separately:

```sh
# macOS (ps is included with the OS)
brew install tmux git

# Ubuntu / Debian / WSL 2
sudo apt install tmux procps git
```

Install and sign in to your chosen agent CLI separately. Update with
`csquad update` (see [Updating](#updating)); remove with
`npm uninstall -g csquad`.
Use one installation channel to avoid competing executables on PATH.
npm enables no completion by itself. Run `csquad completion install` once and
apply the line it prints; see
[npm, npx, and manual installs](usage.md#npm-npx-and-manual-installs).

## macOS / Homebrew

```sh
brew tap ShunL12324/c-squad https://github.com/ShunL12324/c-squad
brew install ShunL12324/c-squad/csquad
```

The source repository also serves as the tap; no separate repository is needed.
Run `brew tap` once, then use the normal install and upgrade commands.
The tap downloads the release binary for your OS and CPU architecture, then
installs tmux, Git, and shell completions. It does not install or sign in to
Claude Code or Codex.

```sh
brew update
brew upgrade csquad
brew uninstall csquad
```

This is a third-party tap, not `homebrew/core`.

## Ubuntu / Debian

Supported package architectures: `amd64` and `arm64`. WSL users follow these
instructions inside their Ubuntu/Debian environment.

Add the signed APT source once:

```sh
sudo apt update
sudo apt install -y ca-certificates curl
sudo install -d -m 0755 /etc/apt/keyrings
curl -fsSL https://shunl12324.github.io/c-squad/apt/key.asc \
  | sudo tee /etc/apt/keyrings/csquad.asc >/dev/null
sudo chmod 0644 /etc/apt/keyrings/csquad.asc

sudo tee /etc/apt/sources.list.d/csquad.sources >/dev/null <<'SOURCE'
Types: deb
URIs: https://shunl12324.github.io/c-squad/apt
Suites: stable
Components: main
Architectures: amd64 arm64
Signed-By: /etc/apt/keyrings/csquad.asc
SOURCE

sudo apt update
sudo apt install csquad
```

APT installs tmux and procps; Git is recommended and normally installed too.
If recommendations are disabled, install Git separately before using code tasks.
Claude Code/Codex installation and authentication remain separate.

```sh
sudo apt update
sudo apt install --only-upgrade csquad
sudo apt remove csquad
```

The repository tracks the current stable release. It is a third-party source,
not part of Ubuntu's or Debian's official archive.

### tmux pane-exit compatibility on Linux

Some Linux tmux builds with libutempter can miss a `pane-died` hook when a pane
exits during a utmp update. This can delay C Squad's response to an engine or
Master exit. [tmux issue #4559](https://github.com/tmux/tmux/issues/4559) was
fixed upstream in [tmux 3.6](https://github.com/tmux/tmux/commit/fa5f3cef3d651b0eb9abfa77fc37ccade81679b5).
Use tmux 3.6 or later, or a distribution build that backports that fix, for
reliable pane-exit hooks when libutempter is enabled. Check `tmux -V` and your
distribution's patch notes; an older version number alone does not establish
whether the fix is present. C Squad does not replace your system tmux.

Alternatively, download a `.deb` from [GitHub Releases](https://github.com/ShunL12324/c-squad/releases/latest) and install it locally:

```sh
sudo apt install ./csquad_VERSION_amd64.deb
```

A local `.deb` alone does not configure automatic updates.

## Standalone archives

Choose `linux` for Linux/WSL, or `darwin` for macOS; select `amd64` for Intel/AMD
or `arm64` for Apple Silicon/ARM64 Linux. Extract the matching `.tar.gz`, then:

```sh
mkdir -p "$HOME/.local/bin"
install -m 755 ./csquad "$HOME/.local/bin/csquad"
```

Add `~/.local/bin` to PATH. Go is not needed for precompiled archives. Install
tmux, ps, and the chosen agent CLI separately; Git is required for code tasks.
Archives include Bash, Zsh, and Fish completion scripts in `completions/`, which
this channel does not enable either; `csquad completion install` does it for
you. See [npm, npx, and manual installs](usage.md#npm-npx-and-manual-installs).

## After installation

```sh
csquad version
csquad doctor
csquad doctor --strict --engine codex   # Or --engine claude
```

Installing a package does not require an engine login and does not create user
configuration. `doctor` checks executable availability, not authentication or
account quota. `--help` and completion generation work without engine binaries.

Uninstalling the program preserves user configuration and project `.csquad/`
recovery data. Delete those separately only when you no longer need them.

## Updating

```sh
csquad update --check   # installed version, channel and latest release; installs nothing
csquad update           # shows the exact commands, asks, then runs them
```

`csquad update` upgrades through the channel that installed csquad. It works
this out from the real path of the running binary and the package manager's
own records:

| Installed with | `update` runs |
|---|---|
| npm (any global prefix, including nvm) | `npm install --global --prefix PREFIX csquad@latest`. If that prefix is not writable, it only prints the `sudo` command. |
| Homebrew (any prefix) | `PREFIX/bin/brew upgrade TAP/csquad`, using the brew that owns the Cellar. |
| The APT source | `sudo apt-get update`, then `sudo apt-get install --only-upgrade csquad`. `apt-get update` refreshes every source on the system. |
| A local `.deb` | Downloads the latest release's `.deb` and `checksums.txt` over HTTPS, verifies the checksum and the package's name, version and architecture, then runs `sudo apt-get install` on it. The checksum file is not signed, so this is weaker than the signed APT source. |
| An archive, a source build, pnpm, yarn or bun | Nothing. It prints the command to use instead. |

- `sudo` runs only in an interactive terminal, after you confirm.
- `--yes` skips the question for the commands that need no `sudo`.
- `update` refuses to run inside a team member's session.
- `--check` and the local `.deb` path query GitHub. If GitHub cannot be
  reached, or its limit of 60 unauthenticated requests per hour is used up,
  `update` says it was unable to query the latest release.

### Running teams keep their version

Installing a new csquad no longer changes a running team:

- Each team runs a private copy of the csquad build it started with, stored
  under `~/.local/share/csquad/versions/` (`$XDG_DATA_HOME` is honoured). Set
  `CSQUAD_VERSIONS_DIR` to use another directory, for example when your home
  is mounted `noexec`.
- A newer `csquad` on PATH hands team commands to the team's own copy.
- `csquad resume` moves a stopped team to the installed version. It refuses
  an older release. It asks first when the two builds cannot be ordered, such
  as development builds or two builds with the same version.
- If a resume fails after it has moved the team, the team stays on the new
  version, and the older build can no longer resume it. Retry with the new
  build; the error names the command.
- If a team's copy is missing or damaged, commands for that team stop with an
  error, and `csquad repin TEAM` is the way out. It restores the copy from an
  identical build, or moves the team to the installed build, stopping it
  first if it is running.
- Old copies are not deleted automatically, because csquad cannot find teams
  in every project. `csquad update --check` shows how much space they use;
  delete unused ones by hand.

**First upgrade to this version.** Teams started by csquad 0.11 or earlier
have no private copy, so an upgrade reaches them as soon as it is installed.
Stop them first, upgrade, then resume them; from then on upgrades leave
running teams alone.

**macOS.** csquad identifies its own build by hashing the file it was started
from, because macOS has no `/proc/self/exe`. A csquad process started from an
install path whose file is replaced in place while it runs is identified by the
new file's bytes. Processes a team starts run from the pinned copy, which never
changes, so they are not affected. macOS has not been tested on real hardware.

**Keep one installation.** A csquad earlier on PATH that predates pinning
still writes teams directly. The same goes for any older csquad: never run it
against a team a newer one has pinned. It does not understand newer records,
drops fields it does not know, such as a task's cancellation reason, and a
resume by it takes the team back to itself. `csquad doctor` lists every csquad
on PATH and warns when there is more than one.

## Building from source

Source builds are for contributors or users testing unreleased changes. See
[Contributing](../CONTRIBUTING.md#development) for the Go toolchain and build steps.
