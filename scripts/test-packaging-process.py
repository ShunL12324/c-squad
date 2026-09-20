#!/usr/bin/env python3
"""Regression checks for packaging-test subprocess deadlines and cleanup."""

import importlib.util
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest import mock

from packaging_test_support import command


spec = importlib.util.spec_from_file_location("shell_completion", Path(__file__).with_name("test-shell-completion.py"))
shell_completion = importlib.util.module_from_spec(spec)
spec.loader.exec_module(shell_completion)


class ProcessTests(unittest.TestCase):
    def test_completion_origin_resolves_alias_and_rejects_other_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            installed = root / "installed"
            installed.mkdir()
            (installed / "_csquad").write_text("installed completion")
            alias = root / "alias"
            alias.symlink_to(installed, target_is_directory=True)
            shell_completion.check_origin(str(alias / "_csquad"), installed)
            for invalid in ("", "_csquad"):
                with self.assertRaises(AssertionError):
                    shell_completion.check_origin(invalid, installed)
            other = root / "_csquad"
            other.write_text("unrelated completion")
            with self.assertRaises(AssertionError):
                shell_completion.check_origin(str(other), installed)
            with self.assertRaises(FileNotFoundError):
                shell_completion.check_origin(str(root / "missing"), installed)

    def test_output_and_expected_failure_are_preserved(self):
        result = command(sys.executable, "-c", "import sys; print('out'); print('err', file=sys.stderr); sys.exit(23)", check=False)
        self.assertEqual((result.returncode, result.stdout, result.stderr), (23, "out\n", "err\n"))
        with self.assertRaises(subprocess.CalledProcessError) as failure:
            command(sys.executable, "-c", "print('failure'); raise SystemExit(7)")
        self.assertEqual(failure.exception.output, "failure\n")

    def test_timeout_kills_descendant_in_another_session(self):
        self.check_timeout_cleanup()

    def test_denied_group_signal_still_reaps_directly_killed_processes(self):
        with mock.patch("packaging_test_support.os.killpg", side_effect=PermissionError("group signal denied")) as group_signal:
            self.check_timeout_cleanup()
            group_signal.assert_not_called()

    def check_timeout_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "child"
            script = ("import os,subprocess,sys,time; from pathlib import Path; "
                      "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'],start_new_session=True); "
                      "Path(sys.argv[1]).write_text(str(child.pid)+' '+str(os.getpid())); time.sleep(30)")
            started = time.monotonic()
            with self.assertRaises(subprocess.TimeoutExpired):
                command(sys.executable, "-c", script, marker, timeout=1)
            self.assertLess(time.monotonic() - started, 8)
            pid, parent = map(int, marker.read_text().split())
            with self.assertRaises(ChildProcessError):
                os.waitpid(parent, os.WNOHANG)
            state = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)], capture_output=True, text=True, timeout=2)
            # Orphans may briefly remain zombies until the system reaper runs.
            self.assertTrue(state.returncode != 0 or state.stdout.strip().startswith("Z"), state.stdout)

    def test_inherited_output_descriptors_do_not_hold_parent_wait(self):
        # A child with an inherited output descriptor must not delay collection
        # of its parent's result. Clean up our fixture explicitly afterward.
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "child"
            script = ("import subprocess,sys; from pathlib import Path; "
                      "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'],start_new_session=True); "
                      "Path(sys.argv[1]).write_text(str(child.pid)); print('parent done')")
            try:
                started = time.monotonic()
                result = command(sys.executable, "-c", script, marker, timeout=2)
                self.assertEqual(result.stdout, "parent done\n")
                self.assertLess(time.monotonic() - started, 3)
            finally:
                if marker.exists():
                    os.kill(int(marker.read_text()), 9)


if __name__ == "__main__":
    unittest.main()
