# Releasing

Stable version tags trigger publication CI. Ordinary pushes and pull requests run
only the CI workflow, which runs `make check` and publishes nothing. The
read-only Prepublish npm workflow can also be triggered manually to validate an
exact candidate before tagging.

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

## Native macOS gate before publication

The `Prepublish npm` workflow has read-only repository permissions and publishes
nothing. It checks out the supplied full commit SHA, verifies HEAD, and uses
GoReleaser snapshot mode to build the four real native binaries. A temporary
configuration sets only the snapshot package version to the intended stable
version so the normal npm packer can consume it; snapshot mode and disabled
publication remain in force. It runs `scripts/test-npm.py` on `macos-14`,
including the real Zsh PTY completion regression. Zsh must exist; this gate must
not silently skip shell validation.

After all changes are integrated and pushed, run it before tagging:

```sh
gh workflow run prepublish-npm.yml --ref main \
  -f sha=APPROVED_FULL_COMMIT_SHA -f version=0.8.0
gh run list --workflow prepublish-npm.yml --event workflow_dispatch
gh run view RUN_ID --log
```

The workflow must first be present on the remote default branch for manual
dispatch. Record the run URL, its workflow head SHA, the checked-out source SHA
from the job summary, version, and native macOS result. For manual dispatch the
run's head SHA identifies the selected workflow ref; the validated input SHA and
checkout check identify the candidate. Any source change requires a new run.
Do not infer macOS success from a Linux fixture or from cross-compilation alone.

The stable-tag Release workflow also calls this same validation and makes its
release job depend on success. A failure therefore blocks creation/publication
of GitHub release assets and all downstream npm, Homebrew, and APT jobs. Existing
post-build channel checks remain in place. This gate builds disposable packages;
the release job still builds and verifies the final distribution assets.

## Publish

Ordinary pushes and pull requests run only `make check`. Before every release:

1. Update [CHANGELOG.md](../CHANGELOG.md) in the release commit. Check each entry
   against the integrated code and acceptance evidence; resolve all pending
   integration entries before tagging. Use the intended version and UTC release
   date, never present a planned release as already published. Keep historical
   dates aligned with GitHub's actual `published_at` timestamps.
2. Fetch the remote default branch and preserve any automated Formula commits.
   Review unexpected remote changes before integrating them; do not force-push.
3. Run `make check` and the native macOS gate above on the final integrated
   source. Confirm the approved exact
   commit is on the remote default branch and that the version is unused in
   Git tags, GitHub Releases, and npm. Tag that explicit commit, not an
   intermediate or moving branch tip.
4. Prepare English GitHub Release notes from the matching changelog section.
   Include the exact source SHA, check the tag/version and changes agree, and
   omit preparation markers. GoReleaser changelog generation is disabled, so
   these notes must be supplied explicitly.

For example, after filling in the approved values:

```sh
RELEASE_TAG=vX.Y.Z
RELEASE_SHA=APPROVED_FULL_COMMIT_SHA
git tag -a "$RELEASE_TAG" "$RELEASE_SHA" -m "C Squad $RELEASE_TAG"
git push origin "$RELEASE_TAG"
```

The stable tag triggers checks, compilation, and publication. When the workflow
creates its draft, apply the prepared notes with
`gh release edit "$RELEASE_TAG" --notes-file /path/to/release-notes.md`.
Recheck the public Release body against that tag's changelog after publication;
do not assume an empty or automatically generated body is sufficient. If the
actual publication crosses a UTC date boundary, correct the changelog date on
the default branch in a follow-up commit; never move the published tag.

After publication, verify GitHub assets and checksums, npm's version and packaged
binaries, the Homebrew Formula and its archive checksums, and the public signed
APT repository. Check installation and reported version/source where supported.
Never overwrite or move a published tag.

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
