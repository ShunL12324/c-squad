#!/usr/bin/env python3
"""Verify installed Zsh completion with a real Tab key in an isolated shell."""

import argparse
import fcntl
import os
from pathlib import Path
import pty
import select
import signal
import struct
import tempfile
import termios
import time


def quote(value):
    """Quote one path for Zsh; test prefixes deliberately contain spaces."""
    return "'" + str(value).replace("'", "'\\''") + "'"


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--path", type=Path,
                        help="Directory prepended to PATH, for a csquad outside the system prefix")
    parser.add_argument("--fpath", type=Path,
                        help="Directory prepended to fpath, for a completion script the shell does not autoload")
    args = parser.parse_args()
    with tempfile.TemporaryDirectory(prefix="csquad-completion-") as temporary:
        origin = Path(temporary) / "origin"
        pid, terminal = pty.fork()
        if pid == 0:
            os.environ["ZDOTDIR"] = temporary
            os.environ["TERM"] = "xterm-256color"
            if args.path:
                os.environ["PATH"] = f"{args.path.resolve()}{os.pathsep}{os.environ['PATH']}"
            os.execvp("zsh", ["zsh", "-f"])
        # A wide window keeps echoed commands on one line, so the markers below
        # cannot be split by wrapping.
        fcntl.ioctl(terminal, termios.TIOCSWINSZ, struct.pack("HHHH", 50, 400, 0, 0))

        def wait_for(expected):
            output = b""
            deadline = time.monotonic() + 15
            while time.monotonic() < deadline:
                ready, _, _ = select.select([terminal], [], [], 0.1)
                if ready:
                    output += os.read(terminal, 65536)
                    if expected in output:
                        return
            raise AssertionError(f"Missing {expected!r} in terminal output: {output!r}")

        # A caller-supplied directory stands in for the fpath line that the user
        # adds to ~/.zshrc; the test must never edit a real shell configuration.
        prologue = f"fpath=({quote(args.fpath.resolve())} $fpath); ".encode() if args.fpath else b""
        try:
            # Load the shell's normal completion system, without sourcing or
            # generating any C Squad completion script explicitly.
            os.write(terminal, prologue + b"unsetopt ZLE; autoload -Uz compinit; compinit -D; "
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
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            os.close(terminal)
        loaded = origin.read_text().strip()
        if args.fpath:
            expected = str(args.fpath.resolve() / "_csquad")
            if loaded != expected:
                raise AssertionError(f"completion came from {loaded!r}, not the installed {expected!r}")
        print(f"PASS: Zsh loaded {loaded} and expands csquad sta + Tab to csquad start")


if __name__ == "__main__":
    main()
