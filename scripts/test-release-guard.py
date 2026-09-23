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


if __name__ == "__main__":
    unittest.main()
