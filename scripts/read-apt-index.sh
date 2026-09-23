#!/usr/bin/env bash
# Fetch the amd64 Packages index the public APT repository currently serves, for
# release-guard.py. Exit 0 with OUTPUT written when an index is deployed, exit 0
# with OUTPUT absent when Pages serves no index yet (the first deployment), and
# fail otherwise: an unknown deployed version must never allow a deployment.
set -euo pipefail
if [ "$#" -ne 2 ]; then
  echo 'Usage: read-apt-index.sh OWNER/REPOSITORY OUTPUT' >&2
  exit 2
fi
repository=$1
output=$2
rm -f "$output"
errors=$(mktemp)
trap 'rm -f "$errors"' EXIT

if ! site=$(gh api "repos/$repository/pages" --jq '.html_url // empty' 2>"$errors"); then
  if grep -q '(HTTP 404)' "$errors"; then
    # deploy-pages cannot deploy either; this is a setup step, not a first release.
    echo "GitHub Pages is not enabled for $repository. Choose GitHub Actions as the Pages" \
      'source (docs/releasing.md, One-time setup), then rerun this job.' >&2
  else
    cat "$errors" >&2
    echo "Cannot read the GitHub Pages configuration of $repository." >&2
  fi
  exit 1
fi
if [ -z "$site" ]; then
  # The API schema does not guarantee html_url. Without it the served version is
  # unknown, so do not deploy over it.
  echo "GitHub Pages for $repository reports no site URL; cannot read the deployed APT index." >&2
  exit 1
fi

# Pages caches for up to ten minutes, so this may be slightly stale. Newer
# versions are published as Releases before they deploy, and release-guard.py
# also compares with those, so a stale index cannot let an older tag through.
url="${site%/}/apt/dists/stable/main/binary-amd64/Packages"
if ! status=$(curl -sS -o "$output" -w '%{http_code}' "$url"); then
  rm -f "$output"
  echo "Cannot read the deployed APT index at $url." >&2
  exit 1
fi
case "$status" in
  200) echo "Deployed APT index: $url" ;;
  404) rm -f "$output"; echo "No APT index is deployed at $url yet." ;;
  *) rm -f "$output"; echo "Cannot read the deployed APT index at $url (HTTP $status)." >&2; exit 1 ;;
esac
