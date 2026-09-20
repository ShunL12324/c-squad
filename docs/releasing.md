# Releasing

Only stable version tags trigger CI. Ordinary pushes and pull requests do not
start a build.

The repository builds archives, a source tarball, and Debian packages with a pinned
GoReleaser version. Homebrew installs precompiled release archives; APT uses signed static
metadata hosted on GitHub Pages. npm packages the same four native binaries in one package. None of these channels
requires a running server.

## One-time setup

1. Publish this repository as `ShunL12324/c-squad`. It also serves as the
   Homebrew tap. The first successful release creates `Formula/csquad.rb` on the
   default branch; later releases update it. `Formula/csquad.rb.in` is the source
   template, not an installable formula.
2. In this repository's Pages settings, choose **GitHub Actions** as the source.
   Allow the `github-pages` environment to deploy release tags.
3. The workflow uses the standard `GITHUB_TOKEN` with `contents: write` to publish
   Releases and commit the formula to this repository's default branch. No
   additional Homebrew token is needed. Branch protection/rulesets must permit
   that workflow commit; otherwise the formula update job will fail.
4. Create a dedicated APT signing key. Store its ASCII-armored private key in
   Actions secret `APT_SIGNING_KEY` and its full fingerprint in Actions variable
   `APT_SIGNING_FINGERPRINT`. The workflow expects an unencrypted signing key,
   imports it into a temporary keyring, and deletes that keyring after signing.
   Retain the key for future releases; replacing it requires users to update trust.

The public key is published at `https://shunl12324.github.io/c-squad/apt/key.asc`.
No maintainer key or credential belongs in the source repository. APT package
installation does not create user configuration or require a model account.

## Local checks

```sh
make check
make snapshot
python3 scripts/render-homebrew.py --repository ShunL12324/c-squad \
  --license MIT --output dist/tap/Formula/csquad.rb
ruby -c dist/tap/Formula/csquad.rb
```

`make snapshot` never publishes. Without Git metadata it stages a temporary source
repository; its commit identifier is synthetic and is not a release commit.
`make test-packaging` exercises signed APT installation, upgrade, and removal in
an Ubuntu container. It requires Docker, GnuPG, apt-utils, and existing snapshot
packages; all keys and package state used by this test are disposable.

## Publish

Ordinary pushes and pull requests run no workflows. Run `make check` locally,
then push a version tag to trigger checks, compilation, and publication:

```sh
git tag -a v0.1.0 -m 'C Squad v0.1.0'
git push origin v0.1.0
```

The Release workflow checks the source, builds a draft, and tests APT
installation, upgrade, and removal before publishing GitHub assets. It then
tests the Formula on a macOS runner and commits it to the default branch.
A separate job deploys the signed APT metadata. Only stable `vX.Y.Z` versions are accepted. The APT source
contains the current release for amd64 and arm64; it is not a historical archive.
Releases are serialized so simultaneous tags cannot overwrite each other's output.

The Release, formula commit, and Pages deployment are not one transaction.
The formula commit is an ordinary branch push and does not trigger another build.
If a formula push or Pages deployment fails after publication, rerun the failed jobs in Actions rather than
rebuilding or overwriting an already published release. Users can still install
the `.deb` or archive from GitHub Releases while a channel is being repaired.

After the first publication, verify:

```sh
brew tap ShunL12324/c-squad https://github.com/ShunL12324/c-squad
brew install ShunL12324/c-squad/csquad
brew test ShunL12324/c-squad/csquad
```

Also verify installation from the public APT URL. Local checks cannot verify
GitHub credentials, Pages configuration, or a remote Homebrew download.

## npm distribution

The npm sources live in `packaging/npm` in this repository. The release workflow
sets the package version from the stable tag; do not manually update the template's
`0.0.0` version. `scripts/build-npm.py` verifies the four archive checksums and
stages the package in `dist/npm/package`. The package has no runtime dependencies
or install scripts. It includes all four binaries to avoid extra registries,
post-install downloads, and multiple platform-package publications.

Before the first automated npm release:

1. Log in to the npm account that will own `csquad` using `npm login`.
2. Stage and test the first package from an existing stable release, then publish
   the generated tarball with `npm publish dist/npm/csquad-VERSION.tgz --access public`.
   Never publish the template directory directly.
3. In the npm package settings, add a GitHub Actions trusted publisher:
   owner **ShunL12324**, repository **c-squad**, workflow **release.yml**,
   with direct publishing allowed and no environment restriction.

Later tags publish via OIDC after npm installation tests pass on Linux and
macOS. No npm token needs to be stored in GitHub. The npm job requires
`id-token: write`; the workflow pins an OIDC-capable npm version.

To test locally with downloaded release archives and `checksums.txt`:

```sh
python3 scripts/build-npm.py --version 0.4.0 --release dist/published-v0.4.0
npm pack ./dist/npm/package --pack-destination dist/npm
python3 scripts/test-npm.py dist/npm/csquad-0.4.0.tgz
```

The staging directory must not already exist. Tests use a temporary npm prefix,
leave the normal installed executable untouched, and disable install scripts.
Native Windows is intentionally unsupported; use WSL 2.

`test-npm.py` also runs `csquad completion install` against a throwaway `HOME`
and then checks, with a real Tab key in an isolated Zsh, that the file it wrote
is the one the shell loads. That last step needs `zsh`; without it the test
prints a `SKIP` line and the rest still runs.

npm versions are immutable. If npm publication fails, fix the authentication or
trusted-publisher configuration and rerun the failed job. Check the registry
before retrying a publish whose outcome is uncertain; do not overwrite a published
version. A failed npm job does not roll back the GitHub, APT, or Homebrew release.
