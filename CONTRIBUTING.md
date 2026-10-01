# Contributing

C Squad is a small, zero-dependency Node package: an installer (`bin/`, `lib/`)
and the files it installs (`payload/`). Keep it small and readable.

```sh
git clone https://github.com/ShunL12324/c-squad.git
cd c-squad
npm test
```

- Tests use `node:test`. They must only touch temporary directories: set
  `CLAUDE_CONFIG_DIR` and `HOME` (`USERPROFILE` on Windows) to one, and put a
  fake `claude` on `PATH` (a `claude.cmd` shim on Windows). CI runs Linux, macOS
  and Windows.
  Never point them at a real `~/.claude`.
- `payload/` is what users get. Skills reference `{{CSQUAD_DIR}}`, which the
  installer replaces with the real path.
- Check `npm pack --dry-run` lists only intended files.

## Releasing

1. Bump `version` in `package.json` and add a `CHANGELOG.md` entry; commit.
2. Tag `vX.Y.Z` (matching `package.json`) and push the tag.
3. `.github/workflows/release.yml` runs the tests and publishes with npm trusted
   publishing (OIDC, provenance). No npm token is stored.

Contributions are provided under the project's [MIT license](LICENSE).
