#!/usr/bin/env bash
# Install the first tmux release with the libutempter pane-exit SIGCHLD fix.
# This is confined to the current CI job; it never replaces the runner's tmux.
set -euo pipefail

: "${RUNNER_TEMP:?RUNNER_TEMP is required}"
: "${GITHUB_PATH:?GITHUB_PATH is required}"

version=3.6
sha256=136db80cfbfba617a103401f52874e7c64927986b65b1b700350b6058ad69607
archive_url="https://github.com/tmux/tmux/releases/download/${version}/tmux-${version}.tar.gz"

sudo apt-get update
sudo apt-get install -y -- ca-certificates curl build-essential bison pkg-config \
  libevent-dev libncurses-dev libutempter-dev "$@"

build_dir=$(mktemp -d "$RUNNER_TEMP/csquad-tmux-build.XXXXXXXX")
trap 'rm -rf -- "$build_dir"' EXIT
archive="$build_dir/tmux-${version}.tar.gz"
curl --fail --location --silent --show-error --retry 3 \
  --connect-timeout 10 --max-time 90 "$archive_url" --output "$archive"
printf '%s  %s\n' "$sha256" "$archive" | sha256sum --check --status
tar -xzf "$archive" -C "$build_dir"

prefix=$(mktemp -d "$RUNNER_TEMP/csquad-tmux-${version}.XXXXXXXX")
(
  cd "$build_dir/tmux-${version}"
  ./configure --prefix="$prefix" --enable-utempter
  make -j 2
  install -D -m 0755 tmux "$prefix/bin/tmux"
)

test "$("$prefix/bin/tmux" -V)" = "tmux $version"
ldd "$prefix/bin/tmux" | grep -q 'libutempter\.so'
printf '%s\n' "$prefix/bin" >> "$GITHUB_PATH"
echo "Installed pinned tmux $version with libutempter at $prefix/bin"
