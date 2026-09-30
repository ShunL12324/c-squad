# Installation

Install a precompiled package with npm, Homebrew, APT, or a download from
[GitHub Releases](https://github.com/ShunL12324/c-squad/releases/latest).
You do not need Go or a compiler.

## npm

```sh
npm install -g csquad
csquad doctor
```

For one-off commands, run `npx csquad`. Install csquad persistently before
queueing tasks, because the dispatcher runs the installed executable. Install
npm inside WSL 2 when using Windows.
The package contains native binaries for macOS and Linux on x64 and arm64;
installation needs no Go compiler, lifecycle scripts, or GitHub downloads.

Install and sign in to Claude Code separately. Update with
`csquad update` (see [Updating](#updating)); remove with
`npm uninstall -g csquad`.
Use one installation channel to avoid competing executables on PATH.
npm enables no completion by itself. Run `csquad completion install` once and
apply the line it prints; see
[Shell completion](#shell-completion).

## macOS / Homebrew

```sh
brew tap ShunL12324/c-squad https://github.com/ShunL12324/c-squad
brew install ShunL12324/c-squad/csquad
```

The source repository also serves as the tap; no separate repository is needed.
Run `brew tap` once, then use the normal install and upgrade commands.
The tap downloads the release binary for your OS and CPU architecture, then
installs Git and shell completions. It does not install or sign in to
Claude Code.

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

Git is recommended and normally installed too. Claude Code installation and
authentication remain separate.

```sh
sudo apt update
sudo apt install --only-upgrade csquad
sudo apt remove csquad
```

The repository tracks the current stable release. It is a third-party source,
not part of Ubuntu's or Debian's official archive.

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
Claude Code separately. Archives include Bash, Zsh, and Fish completion scripts
in `completions/`, which this channel does not enable either; `csquad completion
install` does it for you. See [Shell completion](#shell-completion).

## After installation

```sh
csquad version
csquad doctor
```

Installing a package does not require a Claude Code login and does not create
user configuration. `doctor` checks that `claude` runs and reports the
configuration, database and dispatcher; it does not check authentication or
quota. `--help` and completion generation work without Claude Code.

Uninstalling the program preserves the configuration
(`~/.config/csquad/config.toml`) and the task database
(`~/.local/share/csquad/csquad.db`). Delete them separately only when you no
longer need them.

## Shell completion

Homebrew and APT install Bash, Zsh, and Fish completions automatically. npm,
npx and archive installs need one explicit command:

```sh
csquad completion install           # for your login shell; --shell and --dir override
csquad completion status            # installed files, and the check for each shell
```

It writes the script into a directory you own, rewrites it only when the content
changed, and prints the remaining step instead of editing your shell files:

| Shell | Default target | Remaining step |
| --- | --- | --- |
| Bash | `~/.local/share/bash-completion/completions/csquad` | none, but bash-completion v2 must be installed |
| Zsh | `~/.local/share/zsh/site-functions/_csquad` | run the printed loading line now and add it at the end of `~/.zshrc` |
| Fish | `~/.config/fish/completions/csquad.fish` | none |
| PowerShell | `~/.local/share/csquad/csquad.ps1` | source it from `$PROFILE` |

The installed script finds `csquad` on `PATH` when you press Tab, so it keeps
working across `nvm use`, Node upgrades and `npm install -g csquad@latest`.

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
- `--check` and the local `.deb` path query GitHub. If GitHub cannot be
  reached, or its limit of 60 unauthenticated requests per hour is used up,
  `update` says it was unable to query the latest release.

**Keep one installation.** Different csquad versions on PATH can start
different dispatchers. A running dispatcher keeps its old executable until the
queue empties.

## Building from source

Source builds are for contributors or users testing unreleased changes. See
[Contributing](../CONTRIBUTING.md#development) for the Go toolchain and build steps.
