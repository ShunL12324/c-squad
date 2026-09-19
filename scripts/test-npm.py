#!/usr/bin/env python3
"""Exercise a packed npm release with scripts disabled in an isolated prefix."""

import argparse
import json
import os
import platform
from pathlib import Path
import subprocess
import tarfile
import tempfile


def run(*args, **kwargs):
    return subprocess.run(args, check=True, text=True, capture_output=True, **kwargs).stdout


def test(package):
    with tarfile.open(package) as archive:
        metadata = json.load(archive.extractfile("package/package.json"))
        names = set(archive.getnames())
        assert not metadata.get("scripts"), "installation must not require lifecycle scripts"
        assert not metadata.get("dependencies"), "launcher must not require dependencies"
        for target in ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"):
            member = archive.getmember(f"package/native/{target}/csquad")
            assert member.mode & 0o111, f"non-executable binary: {target}"
        assert all(not name.endswith((".db", ".env")) for name in names)
    version = metadata["version"]
    with tempfile.TemporaryDirectory(prefix="csquad npm test ") as directory:
        root = Path(directory)
        prefix = root / "prefix with spaces"
        run("npm", "install", "--global", "--prefix", str(prefix), "--ignore-scripts",
            "--no-audit", "--no-fund", str(package))
        binary = prefix / "bin/csquad"
        assert f"csquad {version} " in run(str(binary), "version", cwd=root)
        # npm uses a relative bin symlink. An extra absolute link must work too.
        link = root / "linked csquad"
        link.symlink_to(binary)
        assert f"csquad {version} " in run(str(link), "version", cwd=root)
        assert "start" in run(str(binary), "--help")
        assert "csquad" in run(str(binary), "completion", "bash")
        result = subprocess.run([str(binary), "not-a-command"], capture_output=True, text=True)
        assert result.returncode != 0 and "unknown command" in result.stderr
        run("npm", "exec", "--offline", "--yes", "--package", str(package), "--",
            "csquad", "version", cwd=root)
        # Substitute only the packaged binary to check transparent argument,
        # cwd, environment and exit-status forwarding without starting agents.
        native = prefix / "lib/node_modules/csquad/native"
        system = {"Linux": "linux", "Darwin": "darwin"}[platform.system()]
        arch = {"x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
        executable = native / f"{system}-{arch}/csquad"
        executable.write_text('#!/bin/sh\nprintf "%s\\n" "$PWD" "$CSQUAD_NPM_TEST" "$@"\nexit 23\n')
        environment = dict(os.environ, CSQUAD_NPM_TEST="preserved")
        result = subprocess.run([str(binary), "argument with spaces", "--flag"],
                                cwd=root, env=environment, capture_output=True, text=True)
        assert result.returncode == 23
        assert result.stdout.splitlines() == [str(root.resolve()), "preserved", "argument with spaces", "--flag"]
        run("npm", "uninstall", "--global", "--prefix", str(prefix), "csquad",
            "--ignore-scripts", "--no-audit", "--no-fund")
        assert not binary.exists()
    print(f"npm install, exec, launcher forwarding, and uninstall passed for {version}")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("package", type=Path)
    args = parser.parse_args()
    test(args.package.resolve())
