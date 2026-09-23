"""npm first-use guidance and cold/stale Zsh registration in isolated homes."""

import errno
import io
import json
import os
from pathlib import Path
import pty
import select
import shlex
import shutil
import subprocess
import sys
import tarfile
import time

from packaging_test_support import command, kill_tree


def cold_fpath(home, env):
    """Use this Zsh's initialization functions without host completion directories."""
    directory = home / "cold functions"
    directory.mkdir(mode=0o755)
    paths = command("zsh", "-f", "-c", 'print -rl -- $fpath', env=env).stdout.splitlines()
    # Keep one autoloadable completion so compdump does not emit a bare
    # "autoload -Uz" (which prints functions when the empty cache is sourced).
    for name in ("compinit", "compaudit", "compdump", "compinstall", "_main_complete"):
        source = next((Path(path) / name for path in paths if (Path(path) / name).is_file()), None)
        assert source is not None, f"Zsh {name} not found in fpath: {paths}"
        shutil.copyfile(source, directory / name)
        (directory / name).chmod(0o644)
    # No -u/-C: the real compaudit must still reject unsafe fixture permissions.
    # Discover from Zsh itself, so system and Homebrew layouts both work.
    return f"fpath=({shlex.quote(str(directory))}); "


def interactive(binary, *args, env):
    """Run the actual npm launcher on a terminal, not a captured-output shortcut."""
    fd, slave = pty.openpty()
    child = subprocess.Popen([str(binary), *args], stdin=slave, stdout=slave,
                             stderr=slave, env=env, start_new_session=True)
    os.close(slave)
    output = b""
    try:
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            if select.select([fd], [], [], .1)[0]:
                try:
                    chunk = os.read(fd, 65536)
                except OSError as error:
                    if error.errno != errno.EIO:
                        raise
                    break
                if not chunk:
                    break
                output += chunk
        else:
            raise AssertionError(f"interactive npm launcher timed out: {output!r}")
        assert child.wait(timeout=5) == 0, output
    finally:
        if child.poll() is None:
            kill_tree(child.pid)
        os.close(fd)
        child.wait(timeout=5)
    return output.decode(errors="replace")


def check(root, binary, package):
    if not shutil.which("zsh"):
        print("SKIP: Zsh npm first-use and stale-cache checks (zsh unavailable)")
        return
    home = root / "cold zsh home"
    home.mkdir(mode=0o700)
    rc = home / ".zshrc"
    rc.write_text("# no csquad setup\n")
    user = dict(os.environ, HOME=str(home), ZDOTDIR=str(home), SHELL=shutil.which("zsh"),
                XDG_DATA_HOME=str(home / "data"), XDG_CONFIG_HOME=str(home / "config"),
                XDG_STATE_HOME=str(home / "state"), PATH=f"{binary.parent}{os.pathsep}{os.environ['PATH']}")
    for key in list(user):
        if key.startswith("CSQUAD_"):
            user.pop(key)
    dump = home / ".zcompdump"
    # The npm package is installed, but its completion directory is not in fpath.
    # Host fpath may contain csquad or insecure CI/Homebrew directories. Isolate
    # both cold shells without disabling compaudit or preloading compdef.
    prologue = cold_fpath(home, user)
    cold = command("zsh", "-f", "-c", prologue + '''
        autoload -Uz compinit; compinit -d "$ZDOTDIR/.zcompdump"
        print -r -- ${_comps[csquad]:-missing}
    ''', env=user)
    assert cold.stdout.strip() == "missing", cold
    assert not cold.stderr, cold.stderr
    assert dump.is_file(), "cold compinit did not create its cache"
    print("REPRO: npm global install alone leaves Zsh completion missing")
    marker = home / "state/csquad/npm-completion-notice-v1"
    # Agent panes and completion protocol calls must never consume/show the notice.
    assert "one-time setup" not in interactive(binary, "version", env=dict(user, CSQUAD_STATE_DIR="bound"))
    assert "one-time setup" not in interactive(binary, "__complete", "sta", env=user)
    assert not marker.exists()
    first = interactive(binary, "--help", env=user)
    assert "csquad completion install" in first and "one-time setup" in first, first
    assert marker.is_dir()
    assert "one-time setup" not in interactive(binary, "--help", env=user)
    assert rc.read_text() == "# no csquad setup\n"
    installed = command(binary, "completion", "install", "--shell", "zsh", env=user).stdout
    loading = next(line.strip() for line in installed.splitlines() if line.strip().startswith("(( $+functions[compdef]"))
    script = home / "data/zsh/site-functions/_csquad"
    # The printed line also works in a shell with no compinit at all.
    loaded = command("zsh", "-f", "-c", prologue + loading + '; print -r -- ${_comps[csquad]:-missing}',
                     env=user)
    assert loaded.stdout.strip() == "_csquad", loaded
    assert not loaded.stderr, loaded.stderr
    print("PASS: cold Zsh printed instruction registered _csquad without diagnostics")
    assert rc.read_text() == "# no csquad setup\n", "installer modified startup configuration"
    tester = Path(__file__).with_name("test-shell-completion.py")

    def tab(path):
        result = command(sys.executable, tester, "--path", path, "--fpath", script.parent,
                         "--eval", loading, "--compdump", dump, env=user)
        print(result.stdout, end="")

    tab(binary.parent)
    # User adds the printed line; a future shell loads it after its framework.
    rc.write_text('autoload -Uz compinit; compinit -u -d "$ZDOTDIR/.zcompdump"\n' + loading + '\n')
    assert command("zsh", "-i", "-c", 'print -r -- ${_comps[csquad]:-missing}', env=user).stdout.strip() == "_csquad"
    before = script.read_bytes()
    # Exercise an actual npm version replacement with the same tested executable.
    # This derived archive is a local test fixture, never a release artifact.
    upgrade = root / "completion-upgrade.tgz"
    with tarfile.open(package) as source, tarfile.open(upgrade, "w:gz") as target:
        for member in source.getmembers():
            content = source.extractfile(member) if member.isfile() else None
            if member.name == "package/package.json":
                metadata = json.load(content)
                major, minor, patch = map(int, metadata["version"].split("."))
                metadata["version"] = f"{major}.{minor}.{patch + 1}"
                data = json.dumps(metadata).encode()
                content, member.size = io.BytesIO(data), len(data)
            target.addfile(member, content)
    command("npm", "install", "--global", "--prefix", binary.parent.parent,
            "--ignore-scripts", "--no-audit", "--no-fund", package, env=user)
    # Upgrade at the original prefix, then switch to a different Node-style prefix.
    for prefix in (binary.parent.parent, root / "nvm second node prefix"):
        command("npm", "install", "--global", "--prefix", prefix, "--ignore-scripts",
                "--no-audit", "--no-fund", upgrade, env=user)
        installed_metadata = json.loads((prefix / "lib/node_modules/csquad/package.json").read_text())
        assert installed_metadata["version"] == metadata["version"]
        moved = prefix / "bin/csquad"
        assert "one-time setup" not in interactive(moved, "--help", env=user)
        tab(moved.parent)
    command("npm", "uninstall", "--global", "--prefix", root / "nvm second node prefix",
            "--ignore-scripts", "--no-audit", "--no-fund", "csquad", env=user)
    assert script.read_bytes() == before, "Node prefix removal changed user-owned completion"
    print("PASS: npm notice once; cold/current/stale/future Zsh; reinstall, upgrade and Node-prefix switch")
