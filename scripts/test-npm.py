#!/usr/bin/env python3
"""Exercise a packed npm release with scripts disabled in an isolated prefix."""

import argparse
import json
import os
import platform
from pathlib import Path
import shutil
import sys
import tarfile
import tempfile

from packaging_test_support import command, log
from npm_completion_test import check as check_npm_completion


def run(*args, **kwargs):
    return command(*args, **kwargs).stdout


def check_bash_instruction(printed):
    """Run the printed fallback line in real Bash. A path that lost its quoting
    sources nothing and leaves the completion function undefined."""
    if not shutil.which("bash"):
        print("SKIP: bash is missing; the printed source line was not exercised")
        return
    instruction = next(line.strip() for line in printed.splitlines() if line.strip().startswith("source "))
    result = command("bash", "--noprofile", "--norc", "-c",
                             f"{instruction}; declare -F __start_csquad > /dev/null && complete -p csquad", check=False)
    assert result.returncode == 0 and "-F __start_csquad csquad" in result.stdout, (instruction, result)


def check_completion(root, binary):
    """Verify the printed installation/loading path without editing host startup files."""
    home = root / "home with spaces"
    (home / "config").mkdir(parents=True)
    rc = home / ".zshrc"
    rc.write_text("# untouched\n")
    data = home / "data"
    user = dict(os.environ, HOME=str(home), XDG_DATA_HOME=str(data),
                XDG_CONFIG_HOME=str(home / "config"), SHELL="/bin/zsh")
    script = data / "zsh/site-functions/_csquad"
    first = run(str(binary), "completion", "install", env=user)
    assert first.startswith(f"Installed zsh completion: {script}\n"), first
    # The paths here contain spaces on purpose: the printed line is pasted into
    # ~/.zshrc, where an unquoted source path would split into separate words.
    instruction = next(line.strip() for line in first.splitlines() if line.strip().startswith("(( $+functions[compdef]"))
    quoted = "'" + str(script).replace("'", "'\\''") + "'"
    assert instruction.endswith(f"; source {quoted}"), instruction
    again = run(str(binary), "completion", "install", env=user)
    assert again.startswith(f"Unchanged zsh completion: {script}\n"), again
    assert rc.read_text() == "# untouched\n", "installation edited a shell configuration"
    # Bash and Fish install into directories their shells read on their own.
    for shell, target in (("bash", data / "bash-completion/completions/csquad"),
                          ("fish", home / "config/fish/completions/csquad.fish")):
        printed = run(str(binary), "completion", "install", "--shell", shell, env=user)
        assert target.read_text() == run(str(binary), "completion", shell), shell
        if shell == "bash":
            check_bash_instruction(printed)
    status = run(str(binary), "completion", "status", env=user)
    for line in ("zsh         current", "bash        current", "fish        current"):
        assert line in status, status
    if not shutil.which("zsh"):
        print("SKIP: zsh is missing; completion registration was not exercised in a shell")
        return
    # The only proof that matters: a real Tab in a real Zsh, after running the
    # printed instruction verbatim. Passing --eval rather than a line this test
    # composes is what makes a quoting defect in that instruction fail here.
    print(run(sys.executable, str(Path(__file__).resolve().parent / "test-shell-completion.py"),
              "--path", str(binary.parent), "--fpath", str(script.parent),
              "--eval", instruction, env=user), end="")


def test(package):
    log(f"PACKAGE {package} on {platform.system()} {platform.machine()}")
    with tarfile.open(package) as archive:
        metadata = json.load(archive.extractfile("package/package.json"))
        names = set(archive.getnames())
        assert not metadata.get("scripts"), "installation must not require lifecycle scripts"
        assert not metadata.get("dependencies"), "launcher must not require dependencies"
        for name in ("bin/completion-notice.sh", "completions/_csquad",
                     "completions/csquad.bash", "completions/csquad.fish"):
            assert "package/" + name in names, f"missing npm asset: {name}"
        for target in ("darwin-amd64", "darwin-arm64", "linux-amd64", "linux-arm64"):
            member = archive.getmember(f"package/native/{target}/csquad")
            assert member.mode & 0o111, f"non-executable binary: {target}"
        assert all(not name.endswith((".db", ".env")) for name in names)
    version = metadata["version"]
    with tempfile.TemporaryDirectory(prefix="csquad npm test ") as directory:
        root = Path(directory)
        # npm and shell tests must not read/write the host npm config, cache or rc.
        isolated = root / "npm home"
        isolated.mkdir()
        for key in ("HOME", "XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_STATE_HOME", "ZDOTDIR"):
            os.environ[key] = str(isolated / key.lower())
            Path(os.environ[key]).mkdir()
        for key in ("NPM_CONFIG_USERCONFIG", "NPM_CONFIG_GLOBALCONFIG"):
            os.environ[key] = str(isolated / key)
            Path(os.environ[key]).touch()
        os.environ["npm_config_cache"] = str(isolated / "cache")
        os.environ["npm_config_update_notifier"] = "false"
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
        result = command(str(binary), "not-a-command", check=False)
        assert result.returncode != 0 and "unknown command" in result.stderr
        run("npm", "exec", "--offline", "--yes", "--package", str(package), "--",
            "csquad", "version", cwd=root)
        check_completion(root, binary)
        check_npm_completion(root, binary, package)
        # Substitute only the packaged binary to check transparent argument,
        # cwd, environment and exit-status forwarding without starting agents.
        native = prefix / "lib/node_modules/csquad/native"
        system = {"Linux": "linux", "Darwin": "darwin"}[platform.system()]
        arch = {"x86_64": "amd64", "arm64": "arm64", "aarch64": "arm64"}[platform.machine()]
        executable = native / f"{system}-{arch}/csquad"
        executable.write_text('#!/bin/sh\nprintf "%s\\n" "$PWD" "$CSQUAD_NPM_TEST" "$@"\nexit 23\n')
        environment = dict(os.environ, CSQUAD_NPM_TEST="preserved")
        result = command(str(binary), "argument with spaces", "--flag",
                         cwd=root, env=environment, check=False)
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
