# Releasing

Only stable version tags trigger CI. Ordinary pushes and pull requests do not
start a build.

The repository builds archives, a source tarball, and Debian packages with a pinned
GoReleaser version. Homebrew uses a source-based Formula; APT uses signed static
metadata hosted on GitHub Pages. Neither channel requires a running server.

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
