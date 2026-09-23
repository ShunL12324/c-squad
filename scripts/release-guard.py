#!/usr/bin/env python3
"""Decide whether a release run may replace a channel's current version.

APT (GitHub Pages) and the Homebrew Formula only serve one version. Rerunning an
older tag's job after a newer release would roll them back, so a channel is
updated only when the tag is at least as new as everything already published
or deployed there. The same version may be redeployed to repair a failed job.
"""

import argparse
import os
from pathlib import Path
import re
import sys

STABLE = re.compile(r"v?(\d+)\.(\d+)\.(\d+)")


def parse(version):
    match = STABLE.fullmatch(version.strip())
    return tuple(map(int, match.groups())) if match else None


def formula_version(text):
    match = re.search(r'^\s*version\s+"([^"]+)"', text, re.MULTILINE)
    return [match.group(1)] if match else []


def packages_versions(text):
    return re.findall(r"^Version:\s*(\S+)", text, re.MULTILINE)


def decide(version, current):
    """Return (deploy, newest) for a tag and the versions already out there."""
    own = parse(version)
    if own is None:
        raise ValueError(f"expected a stable vX.Y.Z version, got {version!r}")
    # Pre-releases and unrelated tags cannot be the channel's version.
    known = [parsed for parsed in map(parse, current) if parsed]
    newest = max(known, default=None)
    return newest is None or own >= newest, newest


def main():
    parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    parser.add_argument("--version", required=True, help="Tag of this run, for example v0.10.0")
    parser.add_argument("--formula", type=Path, help="Formula currently on the default branch, if any")
    parser.add_argument("--packages", type=Path, help="APT Packages index currently deployed, if any")
    parser.add_argument("--published", nargs="*", default=[], help="Published GitHub Release tags")
    parser.add_argument("--github-output", action="store_true", help="Write deploy=true|false to $GITHUB_OUTPUT")
    args = parser.parse_args()
    current = list(args.published)
    if args.formula and args.formula.exists():
        current += formula_version(args.formula.read_text())
    if args.packages and args.packages.exists():
        current += packages_versions(args.packages.read_text())
    try:
        deploy, newest = decide(args.version, current)
    except ValueError as error:
        sys.exit(f"release guard: {error}")
    newest = "none" if newest is None else "v" + ".".join(map(str, newest))
    if deploy:
        print(f"Deploying {args.version}; newest published or deployed version is {newest}.")
    else:
        print(f"Skipping {args.version}: {newest} is newer and must not be rolled back.")
    if args.github_output:
        with open(os.environ["GITHUB_OUTPUT"], "a") as output:
            output.write(f"deploy={'true' if deploy else 'false'}\n")


if __name__ == "__main__":
    main()
