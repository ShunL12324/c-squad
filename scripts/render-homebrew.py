#!/usr/bin/env python3
"""Render a source-based Homebrew formula from a GoReleaser source artifact."""

import argparse
import hashlib
import json
from pathlib import Path
import re


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--dist", type=Path, default=Path("dist/release"))
    parser.add_argument("--repository", required=True, help="GitHub OWNER/REPO")
    parser.add_argument("--license", required=True, help="Approved SPDX license identifier")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[\w.-]+/[\w.-]+", args.repository):
        parser.error("repository must be OWNER/REPO")
    if not re.fullmatch(r"[A-Za-z0-9.-]+", args.license):
        parser.error("license must be a single SPDX identifier")
    metadata = json.loads((args.dist / "metadata.json").read_text())
    version = metadata["version"]
    if not re.fullmatch(r"\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?", version):
        parser.error("unsupported release version")
    root = Path(__file__).resolve().parent.parent
    source = args.dist / f"csquad_{version}_source.tar.gz"
    values = {
        "REPOSITORY": args.repository,
        "VERSION": version,
        "LICENSE": args.license,
        "SHA256": hashlib.sha256(source.read_bytes()).hexdigest(),
        "MODULE": (root / "go.mod").read_text().split()[1],
        "COMMIT": metadata["commit"],
        "DATE": metadata["date"],
    }
    formula = (root / "packaging/homebrew/csquad.rb.in").read_text()
    for key, value in values.items():
        formula = formula.replace(f"@{key}@", value)
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(formula)


if __name__ == "__main__":
    main()
