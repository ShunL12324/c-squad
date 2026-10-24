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

## npm packages

`csquad` on npm is a small main package (the shim `bin/csquad.js`, from
`npm/csquad/`) with one optional dependency per platform,
`@shunl12324/csquad-{linux,darwin,win32}-{x64,arm64}`, each holding the
binary for its `os`/`cpu`. The shim runs that binary with the same arguments,
stdio, exit status and signals, and sets `CSQUAD_NPM`, so `install` records
that npm installed it and `update` run by the installed copy runs
`npx --yes csquad@latest update`. There is no postinstall step.

```sh
node scripts/build-npm.js 3.0.0                    # every platform, into dist/npm/
node scripts/build-npm.js 3.0.0 --only linux-x64   # one platform, for a local try
```

It builds with `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X
github.com/ShunL12324/c-squad/internal/app.Version=VERSION"` and checks that
the binary for this machine reports VERSION. To try the result without the
registry:

```sh
cd dist/npm && npm pack ./csquad ./csquad-linux-x64
npm i -g --prefix /tmp/csq ./csquad-3.0.0.tgz ./shunl12324-csquad-linux-x64-3.0.0.tgz
/tmp/csq/bin/csquad --version
```

Point `HOME`, `CSQUAD_HOME` and `CLAUDE_CONFIG_DIR` at a temporary directory
before running `install` from there.

## Release

1. Change `## X.Y.Z - unreleased` in `CHANGELOG.md` to the date and commit.
2. Tag and push the tag: `git tag vX.Y.Z && git push origin vX.Y.Z`.
3. `.github/workflows/release.yml` runs `go vet` and `go test` on Linux,
   macOS and Windows, builds the packages for the tag's version, and
   publishes the six platform packages, then `csquad`, with npm trusted
   publishing (`npm publish --provenance --access public`; a version with a
   `-` goes to the `next` dist-tag). A package version already on npm is
   skipped, so a failed run can be re-run. It stops before publishing
   anything if a package name is not on npm yet.
4. Then it creates the GitHub release for the tag, with the version's
   CHANGELOG section as the notes.

Every package name (`csquad` and each `@shunl12324/csquad-<platform>`) has a
trusted publisher on npmjs.com for this repository and `release.yml`. Only a
new package name needs work by hand: npm offers a trusted publisher only for a
package that exists, so publish its first version by hand (`npm publish
--access public` in its `dist/npm` directory), then add the trusted
publisher (package → Settings → Trusted Publisher → GitHub Actions, with
"Allow npm publish").

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
- The status line ledger has golden files in `internal/statusline/testdata`
  (without and with usage cells); after a deliberate layout change,
  `go test ./internal/statusline -run Golden -update` rewrites them; read the
  diff. The same command rewrites the README's status line samples,
  `docs/images/ledger/*-{dark,light}.svg`, which `samples_test.go` renders
  from the real `Layout` (the samples are listed in `readmeSamples`); the
  tests fail while one is out of date. Look at a changed SVG in a browser
  before you commit it.
- `assets/` is what users get. A skill is a directory: `SKILL.md` plus any
  files it reads on demand (create-task's `reference.md`); install copies
  every file. Skills reference `{{CSQUAD}}`, which install replaces with the
  absolute, shell-quoted path of the installed binary, and mark host-only
  parts with `<!-- claude -->` / `<!-- codex -->` blocks.

## README

`README.md` has a Chinese version, `README.zh-CN.md`. When you change one,
change the other in the same commit. The README is also the npm package page,
so use absolute `https://` URLs for images and links to other files.

Contributions are provided under the project's [MIT license](LICENSE).
