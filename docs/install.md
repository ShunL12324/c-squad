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
`npm install -g csquad@latest`; remove with `npm uninstall -g csquad`.
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

Stop teams before upgrading. Uninstalling the program preserves user
configuration and project `.csquad/` recovery data. Delete those separately only
when you no longer need them.

## Building from source

Source builds are for contributors or users testing unreleased changes. See
[Contributing](../CONTRIBUTING.md#development) for the Go toolchain and build steps.
