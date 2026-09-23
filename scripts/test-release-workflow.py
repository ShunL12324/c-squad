#!/usr/bin/env python3
"""Structural checks for the publication order in the Release workflow."""

import json
from pathlib import Path
import subprocess
import unittest

WORKFLOW = Path(__file__).resolve().parent.parent / ".github/workflows/release.yml"


def load(path):
    # Ruby's standard library parses YAML; Python's does not, and the runners
    # and development hosts already need Ruby for the Formula.
    output = subprocess.run(["ruby", "-ryaml", "-rjson", "-e", "puts JSON.dump(YAML.load_file(ARGV[0]))", str(path)],
                            check=True, capture_output=True, text=True, timeout=30).stdout
    return json.loads(output)


def needs(jobs, name):
    """Return every job that must succeed before name runs."""
    direct = jobs[name].get("needs", [])
    direct = [direct] if isinstance(direct, str) else direct
    result = set(direct)
    for job in direct:
        result |= needs(jobs, job)
    return result


def scripts(job):
    return [step.get("run", "") for step in job.get("steps", [])]


class ReleaseWorkflowTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.jobs = load(WORKFLOW)["jobs"]

    def test_release_becomes_public_only_after_npm_tests(self):
        publishing = [name for name, job in self.jobs.items()
                      if any("--draft=false" in run for run in scripts(job))]
        self.assertEqual(publishing, ["publish"])
        self.assertTrue({"prepublish-npm", "release", "npm-test"} <= needs(self.jobs, "publish"))
        self.assertNotIn("publish", needs(self.jobs, "npm-test"))

    def test_channels_wait_for_public_release(self):
        for channel in ("homebrew-test", "homebrew", "apt", "npm"):
            with self.subTest(channel=channel):
                self.assertIn("publish", needs(self.jobs, channel))
        self.assertIn("homebrew-test", needs(self.jobs, "homebrew"))

    def test_single_version_channels_are_guarded(self):
        # Reruns of an older tag must not roll back APT or the Formula (#32).
        for name, uses, script in (("apt", "actions/deploy-pages@", None),
                                   ("homebrew", "actions/download-artifact@", "Update formula in this repository")):
            steps = self.jobs[name]["steps"]
            guard = [step for step in steps if step.get("id") == "guard"]
            self.assertEqual(len(guard), 1, name)
            self.assertIn("scripts/release-guard.py", guard[0]["run"])
            position = steps.index(guard[0])
            guarded = [step for step in steps if step.get("uses", "").startswith(uses)
                       or (script and step.get("name") == script)]
            self.assertTrue(guarded, name)
            for step in guarded:
                with self.subTest(job=name, step=step.get("name") or step.get("uses")):
                    self.assertGreater(steps.index(step), position)
                    self.assertEqual(step.get("if"), "steps.guard.outputs.deploy == 'true'")


if __name__ == "__main__":
    unittest.main()
