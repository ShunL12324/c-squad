#!/usr/bin/env bash
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
releaser=$1
cd "$root"

if git rev-parse --show-toplevel >/dev/null 2>&1; then
  exec "$releaser" release --snapshot --clean
fi

# Source archives have no Git metadata. Stage only build inputs in an isolated
# repository instead of initializing or committing the user's project directory.
stage=$(mktemp -d "${TMPDIR:-/tmp}/csquad-snapshot.XXXXXX")
trap 'rm -rf "$stage"' EXIT
cp -R cmd internal docs scripts Formula .github "$stage/"
cp go.mod go.sum Makefile README.md CONTRIBUTING.md .goreleaser.yaml .golangci.yml .editorconfig .gitignore "$stage/"
if [ -f LICENSE ]; then cp LICENSE "$stage/"; fi
git -C "$stage" init -q
git -C "$stage" remote add origin "https://$(awk '/^module / {print $2}' go.mod).git"
git -C "$stage" add .
git -C "$stage" -c user.name='C-Squad snapshot' -c user.email='snapshot@example.invalid' \
  -c core.hooksPath=/dev/null -c commit.gpgsign=false commit -qm 'Source archive snapshot'
(cd "$stage" && "$releaser" release --snapshot --clean)

# Replace only generated release output; leave other dist artifacts intact.
mkdir -p "$root/dist"
if [ -d "$root/dist/release" ]; then
  rm -rf "$root/dist/release"
fi
cp -R "$stage/dist/release" "$root/dist/release"
printf 'Local snapshot packages: %s/dist/release\n' "$root"
