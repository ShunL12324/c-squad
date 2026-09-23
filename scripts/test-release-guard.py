#!/usr/bin/env python3
"""Checks that reruns of older tags cannot roll back APT or Homebrew."""

import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest


SCRIPT = Path(__file__).with_name("release-guard.py")
spec = importlib.util.spec_from_file_location("release_guard", SCRIPT)
release_guard = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release_guard)


class ReleaseGuardTests(unittest.TestCase):
    def test_decisions(self):
        cases = [
            ("v0.9.0", ["v0.9.1", "v0.9.0"], False),  # the v0.9.0 Pages rerun from #32
            ("v0.9.1", ["v0.9.1", "0.9.0"], True),
            ("v0.9.1", ["0.9.1"], True),              # repairing the same version
            ("v0.10.0", ["v0.9.1"], True),            # numeric, not lexical, order
            ("v0.9.1", ["v0.10.0"], False),
            ("v1.0.0", [], True),                     # first publication
            ("v1.0.0", ["v2.0.0-rc.1", "latest"], True),
        ]
        for version, current, deploy in cases:
            with self.subTest(version=version, current=current):
                self.assertEqual(release_guard.decide(version, current)[0], deploy)

    def test_rejects_unstable_version(self):
        with self.assertRaises(ValueError):
            release_guard.decide("v1.0.0-rc.1", [])

    def test_channel_versions(self):
        formula = 'class Csquad < Formula\n  desc "x"\n  version "0.10.0"\n  license "MIT"\n'
        self.assertEqual(release_guard.formula_version(formula), ["0.10.0"])
        packages = "Package: csquad\nVersion: 0.10.0\nArchitecture: amd64\n\nPackage: csquad\nVersion: 0.9.1\n"
        self.assertEqual(release_guard.packages_versions(packages), ["0.10.0", "0.9.1"])

    def test_command_writes_github_output(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            formula = root / "csquad.rb"
            formula.write_text('  version "0.9.1"\n')
            packages = root / "Packages"
            packages.write_text("Version: 0.9.1\n")
            output = root / "output"
            for version, expected in (("v0.9.0", "deploy=false"), ("v0.9.1", "deploy=true")):
                output.write_text("")
                result = subprocess.run(
                    [sys.executable, SCRIPT, "--version", version, "--formula", formula, "--packages", packages,
                     "--published", "v0.9.0", "--github-output"],
                    env=dict(os.environ, GITHUB_OUTPUT=str(output)), capture_output=True, text=True, check=True)
                self.assertEqual(output.read_text(), expected + "\n", result.stdout)
            # Missing channel files mean nothing is deployed yet.
            output.write_text("")
            subprocess.run([sys.executable, SCRIPT, "--version", "v0.1.0", "--formula", root / "missing.rb",
                            "--packages", root / "missing", "--github-output"],
                           env=dict(os.environ, GITHUB_OUTPUT=str(output)), check=True, capture_output=True)
            self.assertEqual(output.read_text(), "deploy=true\n")


class ReadAptIndexTests(unittest.TestCase):
    """read-apt-index.sh against stub gh and curl; no network is used."""

    SITE = '{"html_url":"https://owner.github.io/repo/"}'

    def run_script(self, pages, status=None, curl_exit=0):
        # pages: JSON body for gh, or "404"/"403" for a gh HTTP failure.
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            bin_dir = root / "bin"
            bin_dir.mkdir()
            (bin_dir / "gh").write_text(
                "#!/bin/sh\n"
                f"case '{pages}' in 404|403) echo 'gh: Failure (HTTP {pages})' >&2; exit 1;; esac\n"
                # Honour the --jq filter the script passes.
                f"printf '%s' '{pages}' | python3 -c 'import json,sys; v=json.load(sys.stdin).get(\"html_url\"); print(v or \"\")'\n")
            curl = "#!/bin/sh\n"
            if curl_exit:
                curl += f"echo 'curl: (6) Could not resolve host' >&2; exit {curl_exit}\n"
            else:
                curl += ('while [ "$1" != -o ]; do shift; done; '
                         f"printf 'Package: csquad\\nVersion: 0.10.0\\n' > \"$2\"; printf {status}\n")
            (bin_dir / "curl").write_text(curl)
            for tool in ("gh", "curl"):
                (bin_dir / tool).chmod(0o755)
            output = root / "Packages"
            output.write_text("stale file from an earlier step")
            result = subprocess.run(["bash", Path(__file__).with_name("read-apt-index.sh"), "owner/repo", output],
                                    env=dict(os.environ, PATH=f"{bin_dir}{os.pathsep}{os.environ['PATH']}"),
                                    capture_output=True, text=True, timeout=30)
            content = output.read_text() if output.exists() else None
            return result, content

    def test_deployed_index_is_kept(self):
        result, content = self.run_script(self.SITE, status=200)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("Version: 0.10.0", content)

    def test_first_deployment_has_no_index(self):
        # Pages is enabled but nothing is deployed: the guard then compares
        # only with published Releases.
        result, content = self.run_script(self.SITE, status=404)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIsNone(content)

    def test_unknown_state_fails_closed(self):
        cases = {
            "pages disabled": (("404",), {}, "GitHub Pages is not enabled"),
            "no permission": (("403",), {}, "Cannot read the GitHub Pages configuration"),
            "no site url": (("{}",), {"status": 200}, "reports no site URL"),
            "server error": ((self.SITE,), {"status": 503}, "HTTP 503"),
            "network error": ((self.SITE,), {"curl_exit": 6}, "Cannot read the deployed APT index"),
        }
        for name, (args, kwargs, message) in cases.items():
            with self.subTest(name):
                result, content = self.run_script(*args, **kwargs)
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)
                self.assertIsNone(content)


if __name__ == "__main__":
    unittest.main()
