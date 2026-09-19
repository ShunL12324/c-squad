#!/usr/bin/env python3
"""Verify installed Zsh completion with a real Tab key in an isolated shell."""

import os
import pty
import select
import signal
import tempfile
import time


def main():
    with tempfile.TemporaryDirectory(prefix="csquad-completion-") as temporary:
        pid, terminal = pty.fork()
        if pid == 0:
            os.environ["ZDOTDIR"] = temporary
            os.environ["TERM"] = "xterm-256color"
            os.execvp("zsh", ["zsh", "-f"])

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

        try:
            # Load the shell's normal completion system, without sourcing or
            # generating any C Squad completion script explicitly.
            os.write(terminal, b"unsetopt ZLE; autoload -Uz compinit; compinit -D; "
                     b"PROMPT=''; setopt ZLE; print CSQ_READY\n")
            wait_for(b"\r\nCSQ_READY\r\n")
            os.write(terminal, b"function csq_buffer() { print -r -- \"CSQ_BUFFER=$BUFFER\"; "
                     b"BUFFER=''; zle redisplay; }; zle -N csq_buffer; "
                     b"bindkey '^X' csq_buffer; print CSQ_BOUND\n")
            wait_for(b"\r\nCSQ_BOUND\r\n")
            os.write(terminal, b"csquad sta\t\x18")
            wait_for(b"CSQ_BUFFER=csquad start ")
            print("PASS: installed Zsh completion expands csquad sta + Tab to csquad start")
        finally:
            os.kill(pid, signal.SIGKILL)
            os.waitpid(pid, 0)
            os.close(terminal)


if __name__ == "__main__":
    main()
