# Contributing

csquad 3 is one Go binary (`cmd/csquad`) that carries everything it installs:
the worker rules and the skills are embedded from `assets/`. Keep it small and
readable.

## Build

Go 1.26 or newer, no C toolchain (`CGO_ENABLED=0`; SQLite is
`modernc.org/sqlite`):

```sh
git clone https://github.com/ShunL12324/c-squad.git
cd c-squad
CGO_ENABLED=0 go build -o csquad ./cmd/csquad
./csquad install     # copies itself to ~/.csquad/bin/csquad, installs the skills, wraps the status line
```

Cross-compile with `GOOS`/`GOARCH` (darwin, linux, windows × amd64, arm64),
e.g. `GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -o csquad.exe ./cmd/csquad`.
`-ldflags "-X github.com/ShunL12324/c-squad/internal/app.Version=3.0.0"` sets
the version.

npm packaging (a small main package plus per-platform optional packages with
the binary, so `npx csquad@latest install` keeps working) is a later
milestone; until then there is no release pipeline.

## Test

```sh
go vet ./...
GOOS=windows go vet ./...
go test ./...
```

- Tests only touch temporary directories: `CSQUAD_HOME`, `CLAUDE_CONFIG_DIR`
  and `HOME` point into `t.TempDir()`, and `claude` on `PATH` is a fake (the
  test binary itself, see `internal/testutil`). Never point them at a real
  `~/.claude` or `~/.csquad`, and never start the real `claude`.
- Codex tests use `internal/engine/codex/codextest`: an in-process fake
  app-server on a Unix socket in a temporary directory, a temporary
  `CODEX_HOME`, and a fake `codex` (the test binary) whose
  `app-server daemon start` links the control socket to it. Never point them
  at the real `~/.codex` or start the real `codex`.
- Opt-in live tests: `CSQUAD_CODEX_LIVE=1 go test ./internal/engine/codex
  -run Live` runs one trivial thread on the real Codex daemon (your login and
  configuration) and deletes it afterwards.
- The status line ledger has golden files in `internal/statusline/testdata`;
  after a deliberate layout change, `go test ./internal/statusline -run
  TestGolden -update` rewrites them; read the diff.
- `assets/` is what users get. Skills reference `{{CSQUAD}}`, which install
  replaces with the absolute, shell-quoted path of the installed binary.

Contributions are provided under the project's [MIT license](LICENSE).
