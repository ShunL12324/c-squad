# Contributing

C Squad coordinates real local processes and Git workspaces. Changes should keep
that behavior predictable, observable, and recoverable.

## Development

Use Go 1.26+, Git, and tmux on Linux, macOS, or WSL 2.

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
real user's team, configuration, or worktree. Integration tests use real tmux and
Git with fake agent processes, so the suite does not require paid model accounts.

## Changes

Open an issue for substantial behavior changes before implementing them. For a
bug report, include the OS, tmux/engine versions, command, expected result, and
observed result. Remove credentials and private conversation content.

Keep pull requests focused. Explain the user-visible change, how it was tested,
and any compatibility limitations. Prefer tests of observable behavior over tests
that mirror implementation details. Preserve existing task/message encoding and
recovery semantics unless the change includes a migration.

See [architecture](docs/architecture.md) for package boundaries and invariants,
and [releasing](docs/releasing.md) for distribution maintenance.

Contributions are provided under the project's [MIT license](LICENSE).
