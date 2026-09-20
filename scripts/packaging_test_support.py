"""Bounded subprocesses and diagnostics for isolated packaging tests."""

import os
import shlex
import signal
import subprocess
import sys
import tempfile
import time


def log(message):
    print(f"[packaging-test] {message}", file=sys.stderr, flush=True)


def kill_tree(pid):
    """Stop this test child's descendants, including children in new sessions."""
    snapshot = subprocess.run(["ps", "-axo", "pid=,ppid="], check=True,
                              capture_output=True, text=True, timeout=5)
    parents = {}
    for line in snapshot.stdout.splitlines():
        child, parent = map(int, line.split())
        parents.setdefault(parent, []).append(child)
    descendants = []

    def collect(parent):
        for child in parents.get(parent, []):
            descendants.append(child)
            collect(child)

    collect(pid)
    for child in reversed(descendants):
        try:
            os.kill(child, signal.SIGKILL)
        except ProcessLookupError:
            pass
    try:
        os.kill(pid, signal.SIGKILL)
    except ProcessLookupError:
        pass


def command(*args, timeout=60, check=True, **kwargs):
    """Keep original output available for assertions, with finite wait/cleanup."""
    args = tuple(map(str, args))
    label = shlex.join(args)
    started = time.monotonic()
    log(f"RUN {label}")
    # Regular files cannot keep communicate() blocked when a grandchild inherits
    # a pipe after the original process dies (PTY children may create sessions).
    with tempfile.TemporaryFile(mode="w+") as stdout, tempfile.TemporaryFile(mode="w+") as stderr:
        process = subprocess.Popen(args, stdout=stdout, stderr=stderr,
                                   text=True, start_new_session=True, **kwargs)
        timed_out = False
        try:
            process.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            timed_out = True
            log(f"TIMEOUT after {timeout}s: pid={process.pid} {label}")
            try:
                kill_tree(process.pid)
            except (OSError, subprocess.SubprocessError):
                # A redundant group signal after successful tree cleanup
                # returned EPERM on the macOS runner.
                # Keep the group fallback only for failed tree cleanup.
                try:
                    os.killpg(process.pid, signal.SIGKILL)
                except ProcessLookupError:
                    pass
                raise
            finally:
                process.wait(timeout=5)
        stdout.seek(0)
        stderr.seek(0)
        result = subprocess.CompletedProcess(args, process.returncode, stdout.read(), stderr.read())
    log(f"EXIT {result.returncode} after {time.monotonic() - started:.2f}s: {label}")
    if result.stderr:
        print(result.stderr, file=sys.stderr, end="", flush=True)
    if timed_out or (check and result.returncode):
        if result.stdout:
            print(result.stdout, file=sys.stderr, end="", flush=True)
        if timed_out:
            raise subprocess.TimeoutExpired(args, timeout, result.stdout, result.stderr)
        result.check_returncode()
    return result
