# Installation

Package channels become available after the first public release. Until then,
follow the [source quick start](../README.md#quick-start) with Go 1.26+.

## macOS / Homebrew

```sh
brew tap ShunL12324/c-squad https://github.com/ShunL12324/c-squad
brew install ShunL12324/c-squad/csquad
```

The source repository also serves as the tap; no separate repository is needed.
Run `brew tap` once, then use the normal install and upgrade commands.
The tap uses a standard source-based Formula. Homebrew installs Go as a build
dependency, compiles the CLI, and installs tmux, Git, and shell completions.
The first installation may take a few minutes. It does not install or sign in to
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

Alternatively, download a `.deb` from GitHub Releases and install it locally:

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
Archives include Bash, Zsh, and Fish completion scripts in `completions/`.

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
