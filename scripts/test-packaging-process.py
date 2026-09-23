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

    def test_slow_child_startup_retries_until_descendant_exists(self):
        # A loaded runner can take longer than the first deadline to start the
        # child; the check must then retry instead of reading a missing marker.
        self.check_timeout_cleanup(startup_delay=1.5)

    def test_denied_group_signal_still_reaps_directly_killed_processes(self):
        with mock.patch("packaging_test_support.os.killpg", side_effect=PermissionError("group signal denied")) as group_signal:
            self.check_timeout_cleanup()
            group_signal.assert_not_called()

    def check_timeout_cleanup(self, startup_delay=0):
        # The deadline must expire after the descendant exists. Startup time is
        # unbounded on shared runners, so retry with a longer deadline until the
        # child reports its descendant. A child killed earlier is still reaped by
        # command(), and a descendant spawned before the marker is its child too.
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "child"
            script = ("import os,subprocess,sys,time; from pathlib import Path; "
                      f"time.sleep({startup_delay}); "
                      "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(30)'],start_new_session=True); "
                      "partial=Path(sys.argv[1]+'.partial'); partial.write_text(str(child.pid)+' '+str(os.getpid())); "
                      "os.replace(partial,sys.argv[1]); time.sleep(30)")
            for timeout in (1, 2, 4, 8, 16):
                started = time.monotonic()
                with self.assertRaises(subprocess.TimeoutExpired):
                    command(sys.executable, "-c", script, marker, timeout=timeout)
                # Cleanup after the deadline must itself be prompt.
                self.assertLess(time.monotonic() - started, timeout + 7)
                if marker.exists():
                    break
            else:
                self.fail("child never reported its descendant before the deadline")
            pid, parent = map(int, marker.read_text().split())
            with self.assertRaises(ChildProcessError):
                os.waitpid(parent, os.WNOHANG)
            state = subprocess.run(["ps", "-o", "stat=", "-p", str(pid)], capture_output=True, text=True, timeout=2)
            # Orphans may briefly remain zombies until the system reaper runs.
            self.assertTrue(state.returncode != 0 or state.stdout.strip().startswith("Z"), state.stdout)

    def test_inherited_output_descriptors_do_not_hold_parent_wait(self):
        # A child with an inherited output descriptor must not delay collection
        # of its parent's result. The deadline only bounds a regression; the
        # proof is that the descendant is still running when the result returns,
        # so a slow parent startup cannot fail this check. Clean up afterward.
        with tempfile.TemporaryDirectory() as directory:
            marker = Path(directory) / "child"
            script = ("import subprocess,sys; from pathlib import Path; "
                      "child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(300)'],start_new_session=True); "
                      "Path(sys.argv[1]).write_text(str(child.pid)); print('parent done')")
            try:
                result = command(sys.executable, "-c", script, marker, timeout=60)
                self.assertEqual(result.stdout, "parent done\n")
                # Signal 0 raises if the descendant already exited or was reaped.
                os.kill(int(marker.read_text()), 0)
            finally:
                if marker.exists():
                    try:
                        os.kill(int(marker.read_text()), 9)
                    except ProcessLookupError:
                        pass

if __name__ == "__main__":
    unittest.main()
