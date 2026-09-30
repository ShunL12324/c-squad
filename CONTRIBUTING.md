# Contributing

C Squad queues work for real Claude Code sessions. Changes should keep that
behavior predictable and observable, and leave everything Claude Code already
does to Claude Code.

## Development

Use Go 1.26+ and Git on Linux, macOS, or WSL 2.

```sh
git clone https://github.com/ShunL12324/c-squad.git
cd c-squad
make fmt
make check
make build
```

Run `bin/csquad` to try your build. Use `make install` to install it under
`~/.local/bin`; add that directory to your PATH.

Tests use Go's standard `testing` package. Keep unit tests beside the code they
exercise. Use temporary directories and cleanup handlers; never point tests at a
real user's configuration, queue database or Claude Code state; set
`XDG_DATA_HOME`, `XDG_CONFIG_HOME` and `CLAUDE_CONFIG_DIR` to temporary
directories. Tests replace `claude` with a fake (`CSQUAD_CLAUDE`), so the suite
does not require a paid model account.

## Changes

Open an issue for substantial behavior changes before implementing them. For a
bug report, include the OS, Claude Code version, command, expected result, and
observed result. Remove credentials and private conversation content.

Keep pull requests focused. Explain the user-visible change, how it was tested,
and any compatibility limitations. Prefer tests of observable behavior over tests
that mirror implementation details. Preserve the queue database schema unless the
change includes a migration.

See [the design](docs/redesign.md) for the model and its boundaries,
and [releasing](docs/releasing.md) for distribution maintenance.

Contributions are provided under the project's [MIT license](LICENSE).
