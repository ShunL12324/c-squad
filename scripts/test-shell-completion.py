#!/usr/bin/env python3
"""Verify installed Zsh completion with a real Tab key in an isolated shell."""

import argparse
import fcntl
import os
from pathlib import Path
import pty
import select
import struct
import tempfile
import termios
import time

from packaging_test_support import kill_tree, log


def quote(value):
    """Quote one path for Zsh; test prefixes deliberately contain spaces."""
    return "'" + str(value).replace("'", "'\\''") + "'"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--path", type=Path,
                        help="Directory prepended to PATH, for a csquad outside the system prefix")
    parser.add_argument("--fpath", type=Path,
                        help="Directory prepended to fpath, for a completion script the shell does not autoload")
    parser.add_argument("--eval", dest="evaluate",
                        help="Zsh line run verbatim before compinit; use it to execute the exact"
                             " instruction csquad printed instead of a line this script composes")
    args = parser.parse_args()
    if args.evaluate and not args.fpath:
        parser.error("--eval needs --fpath, the directory the completion must come from")
    with tempfile.TemporaryDirectory(prefix="csquad-completion-") as temporary:
        origin = Path(temporary) / "origin"
        log("PTY fork: starting")
        pid, terminal = pty.fork()
        if pid == 0:
            os.environ["ZDOTDIR"] = temporary
            os.environ["TERM"] = "xterm-256color"
            if args.path:
                os.environ["PATH"] = f"{args.path.resolve()}{os.pathsep}{os.environ['PATH']}"
            os.execvp("zsh", ["zsh", "-f"])
        log(f"PTY fork: child pid={pid}, fd={terminal}")
        # A wide window keeps echoed commands on one line, so the markers below
        # cannot be split by wrapping.
        fcntl.ioctl(terminal, termios.TIOCSWINSZ, struct.pack("HHHH", 50, 400, 0, 0))

        def wait_for(expected):
            log(f"PTY waiting for {expected!r}")
            output = b""
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                ready, _, _ = select.select([terminal], [], [], 0.1)
                if ready:
                    output += os.read(terminal, 65536)
                    if expected in output:
                        log(f"PTY received {expected!r}")
                        return
            raise AssertionError(f"Missing {expected!r} in terminal output: {output!r}")

        # These stand in for the fpath line the user adds to ~/.zshrc; the test
        # must never edit a real shell configuration. --eval runs the printed
        # instruction unmodified, so a quoting defect in it fails the test.
        prologue = b""
        if args.evaluate:
            prologue = args.evaluate.encode() + b"; "
        elif args.fpath:
            prologue = f"fpath=({quote(args.fpath.resolve())} $fpath); ".encode()
        try:
            # Load the shell's normal completion system, without sourcing or
            # generating any C Squad completion script explicitly.
            # -u accepts group-writable temporary directories: without it compinit
            # stops for an interactive prompt and the run times out instead of
            # reporting what actually went wrong.
            os.write(terminal, prologue + b"unsetopt ZLE; autoload -Uz compinit; compinit -D -u; "
                     b"PROMPT=''; setopt ZLE; print CSQ_READY\n")
            wait_for(b"\r\nCSQ_READY\r\n")
            os.write(terminal, b"function csq_buffer() { print -r -- \"CSQ_BUFFER=$BUFFER\"; "
                     b"BUFFER=''; zle redisplay; }; zle -N csq_buffer; "
                     b"bindkey '^X' csq_buffer; print CSQ_BOUND\n")
            wait_for(b"\r\nCSQ_BOUND\r\n")
            os.write(terminal, b"csquad sta\t\x18")
            wait_for(b"CSQ_BUFFER=csquad start ")
            # Report the file the completion function came from through a file
            # rather than the terminal, so no echoed command can be mistaken for
            # the answer. Another csquad on the system must not satisfy the test.
            os.write(terminal, b"print -r -- ${functions_source[_csquad]} > "
                     + quote(origin).encode() + b"; print CSQ_ORIGIN\n")
            wait_for(b"\r\nCSQ_ORIGIN\r\n")
        finally:
            log(f"PTY cleanup: pid={pid}")
            try:
                kill_tree(pid)
                deadline = time.monotonic() + 5
                while os.waitpid(pid, os.WNOHANG)[0] == 0:
                    if time.monotonic() >= deadline:
                        raise RuntimeError(f"PTY child {pid} did not exit after SIGKILL")
                    time.sleep(0.01)
            finally:
                os.close(terminal)
            log("PTY cleanup: complete")
        loaded = origin.read_text().strip()
        if args.fpath:
            expected = str(args.fpath.resolve() / "_csquad")
            if loaded != expected:
                raise AssertionError(f"completion came from {loaded!r}, not the installed {expected!r}")
        print(f"PASS: Zsh loaded {loaded} and expands csquad sta + Tab to csquad start")


if __name__ == "__main__":
    main()
