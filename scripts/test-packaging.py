#!/usr/bin/env python3
"""Exercise signed APT install, upgrade and removal in a disposable Ubuntu container."""

import argparse
import os
from pathlib import Path
import shutil
import subprocess
import tempfile


def run(*args, **kwargs):
    return subprocess.run(args, check=True, **kwargs)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--image", default="ubuntu:24.04")
    args = parser.parse_args()
    root = Path(__file__).resolve().parent.parent
    packages = sorted((root / "dist/release").glob("*.deb"))
    if len(packages) != 2:
        parser.error("run make snapshot first; expected amd64 and arm64 Debian packages")
    with tempfile.TemporaryDirectory(prefix="csquad-packaging-") as temporary:
        stage = Path(temporary)
        keyring = stage / "keyring"
        keyring.mkdir(mode=0o700)
        env = dict(os.environ, GNUPGHOME=str(keyring))
        try:
            run("gpg", "--batch", "--passphrase", "", "--quick-generate-key",
                "C Squad disposable test <test@example.invalid>", "ed25519", "sign", "1d", env=env)
            keys = subprocess.check_output(["gpg", "--with-colons", "--list-secret-keys"], env=env, text=True)
            fingerprint = next(line.split(":")[9] for line in keys.splitlines() if line.startswith("fpr:"))
            run("bash", str(root / "scripts/build-apt-repo.sh"), str(root / "dist/release"), str(stage / "repo1"), fingerprint, env=env)
            updated = stage / "updated"
            updated.mkdir()
            for package in packages:
                unpacked = stage / "unpacked"
                run("dpkg-deb", "--raw-extract", str(package), str(unpacked))
                control = unpacked / "DEBIAN/control"
                lines = control.read_text().splitlines()
                lines = [line + "+upgrade1" if line.startswith("Version:") else line for line in lines]
                control.write_text("\n".join(lines) + "\n")
                run("dpkg-deb", "--build", "--root-owner-group", str(unpacked), str(updated / package.name))
                shutil.rmtree(unpacked)
            run("bash", str(root / "scripts/build-apt-repo.sh"), str(updated), str(stage / "repo2"), fingerprint, env=env)
            # apt's unprivileged downloader must traverse the mounted directories.
            stage.chmod(0o755)
            script = r'''
set -eu
printf '%s\n' 'deb [signed-by=/repo1/key.asc] file:/repo1 stable main' > /etc/apt/sources.list.d/csquad.list
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq csquad zsh python3
csquad version
csquad --help >/dev/null
command -v tmux
command -v ps
for f in /usr/share/bash-completion/completions/csquad /usr/share/zsh/vendor-completions/_csquad /usr/share/fish/vendor_completions.d/csquad.fish /usr/share/doc/csquad/copyright; do test -s "$f"; done
python3 /test-shell-completion.py
# Package installation must not eagerly create user configuration.
test ! -e /root/.config/csquad/config.toml
csquad config >/dev/null
printf '\n# keep user changes\n' >> /root/.config/csquad/config.toml
mkdir -p /tmp/project/.csquad
printf 'keep recovery data\n' > /tmp/project/.csquad/sentinel
old=$(dpkg-query -W -f='${Version}' csquad)
printf '%s\n' 'deb [signed-by=/repo2/key.asc] file:/repo2 stable main' > /etc/apt/sources.list.d/csquad.list
apt-get update -qq
DEBIAN_FRONTEND=noninteractive apt-get install -y -qq --only-upgrade csquad
new=$(dpkg-query -W -f='${Version}' csquad)
dpkg --compare-versions "$new" gt "$old"
apt-get purge -y -qq csquad
test ! -e /usr/bin/csquad
grep -q 'keep user changes' /root/.config/csquad/config.toml
test -s /tmp/project/.csquad/sentinel
printf 'PASS: signed APT install, upgrade, purge, config and recovery preservation\n'
'''
            run("docker", "run", "--rm", "-v", f"{stage / 'repo1'}:/repo1:ro",
                "-v", f"{stage / 'repo2'}:/repo2:ro",
                "-v", f"{root / 'scripts/test-shell-completion.py'}:/test-shell-completion.py:ro",
                "-v", f"{root / 'scripts/packaging_test_support.py'}:/packaging_test_support.py:ro",
                args.image, "bash", "-c", script)
        finally:
            subprocess.run(["gpgconf", "--kill", "gpg-agent"], env=env, check=False)


if __name__ == "__main__":
    main()
