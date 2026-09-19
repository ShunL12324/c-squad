#!/usr/bin/env python3
"""Stage one npm package from the four checksummed GoReleaser archives."""

import argparse
import hashlib
import json
from pathlib import Path
import re
import shutil
import tarfile

ROOT = Path(__file__).resolve().parent.parent
PLATFORMS = ("linux_amd64", "linux_arm64", "darwin_amd64", "darwin_arm64")


def stage(release, output, version):
    if not re.fullmatch(r"\d+\.\d+\.\d+", version):
        raise ValueError("npm releases require a stable X.Y.Z version")
    checksums = {}
    for line in (release / "checksums.txt").read_text().splitlines():
        digest, name = line.split(maxsplit=1)
        checksums[name.lstrip("*")] = digest
    # Verify the entire input set before writing a package.
    for platform in PLATFORMS:
        name = f"csquad_{version}_{platform}.tar.gz"
        digest = hashlib.sha256((release / name).read_bytes()).hexdigest()
        if checksums.get(name) != digest:
            raise ValueError(f"checksum mismatch: {name}")
    output.mkdir(parents=True, exist_ok=False)
    shutil.copytree(ROOT / "packaging/npm", output, dirs_exist_ok=True)
    shutil.copy2(ROOT / "LICENSE", output / "LICENSE")
    metadata = json.loads((output / "package.json").read_text())
    metadata.pop("private", None)
    metadata["version"] = version
    (output / "package.json").write_text(json.dumps(metadata, indent=2) + "\n")
    for platform in PLATFORMS:
        with tarfile.open(release / f"csquad_{version}_{platform}.tar.gz") as archive:
            member = archive.getmember("csquad")
            if not member.isfile():
                raise ValueError(f"not a regular binary: {platform}")
            binary = output / "native" / platform.replace("_", "-") / "csquad"
            binary.parent.mkdir(parents=True)
            with archive.extractfile(member) as source, binary.open("wb") as target:
                shutil.copyfileobj(source, target)
            binary.chmod(0o755)
            if platform == PLATFORMS[0]:
                for name in ("csquad.bash", "_csquad", "csquad.fish"):
                    member = archive.getmember(f"completions/{name}")
                    if not member.isfile():
                        raise ValueError(f"not a regular completion: {name}")
                    destination = output / "completions" / name
                    destination.parent.mkdir(exist_ok=True)
                    with archive.extractfile(member) as source:
                        destination.write_bytes(source.read())
    print(output)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--release", type=Path, default=ROOT / "dist/release")
    parser.add_argument("--output", type=Path, default=ROOT / "dist/npm/package")
    parser.add_argument("--version", required=True)
    args = parser.parse_args()
    stage(args.release, args.output, args.version)
