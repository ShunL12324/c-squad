#!/usr/bin/env python3
"""Write GitHub Release notes from the matching CHANGELOG.md section."""

import argparse
from pathlib import Path
import re
import sys

# "## v1.2.3 — 2026-09-23" or "## [v1.2.3](url) — 2026-09-23".
HEADING = re.compile(r"^## (?:\[(v\d+\.\d+\.\d+)\]\([^)]*\)|(v\d+\.\d+\.\d+)) — (\S.*)$")
# Preparation markers that must be resolved before a version is tagged.
MARKERS = re.compile(r"^#+ .*\b(?:Unreleased|Pending integration)\b", re.IGNORECASE | re.MULTILINE)


def notes(changelog, version, sha):
    """Return the release body for version, or raise ValueError."""
    if not re.fullmatch(r"v\d+\.\d+\.\d+", version):
        raise ValueError(f"expected a stable vX.Y.Z version, got {version!r}")
    if not re.fullmatch(r"[0-9a-f]{40}", sha):
        raise ValueError(f"expected a full 40-character commit SHA, got {sha!r}")
    section = None
    lines = []
    for line in changelog.splitlines():
        if line.startswith("## "):
            if section is not None:
                break
            match = HEADING.match(line)
            if match and (match.group(1) or match.group(2)) == version:
                section = match.group(3)
            continue
        if section is not None:
            lines.append(line)
    if section is None:
        raise ValueError(f"CHANGELOG.md has no section for {version}")
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", section):
        raise ValueError(f"{version} section must carry its UTC release date, got {section!r}")
    body = "\n".join(lines).strip()
    if not body:
        raise ValueError(f"{version} section is empty")
    marker = MARKERS.search(body)
    if marker:
        raise ValueError(f"{version} section still contains preparation marker {marker.group(0)!r}")
    return f"{body}\n\nSource commit: {sha}\n"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--version", required=True, help="Stable tag, for example v0.10.0")
    parser.add_argument("--sha", required=True, help="Full source commit SHA of the tag")
    parser.add_argument("--changelog", type=Path, default=Path("CHANGELOG.md"))
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    try:
        text = notes(args.changelog.read_text(), args.version, args.sha)
    except ValueError as error:
        sys.exit(f"release notes: {error}")
    args.output.write_text(text)


if __name__ == "__main__":
    main()
