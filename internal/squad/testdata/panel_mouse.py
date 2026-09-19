"""Exercise single-click navigation through real tmux clients and pseudo-terminals."""
import fcntl
import os
import pty
import select
import struct
import subprocess
import sys
import termios
import time

socket, master, worker = sys.argv[1:]
clients = []
env = dict(os.environ, TERM="xterm-256color")
for key in ("TMUX", "TMUX_PANE"):
    env.pop(key, None)


def tm(*args):
    return subprocess.check_output(["tmux", "-S", socket, *args], text=True).strip()


def drain(seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        ready, _, _ = select.select([entry[0] for entry in clients], [], [], .01)
        for fd in ready:
            os.read(fd, 65536)


def sessions():
    return dict(row.split("|") for row in tm("list-clients", "-F", "#{client_name}|#{session_name}").splitlines())


try:
    for _ in range(2):
        fd, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 180, 0, 0))
        name = os.ttyname(slave)
        child = subprocess.Popen(["tmux", "-S", socket, "attach", "-t", master],
                                 stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
        os.close(slave)
        clients.append((fd, name, child))
    drain(1)
    fd, client, _ = clients[0]
    observer = clients[1][1]
    # Same coordinates within the double-click interval exercise all three
    # tmux click bindings. Another attached client makes origin tracking necessary.
    for attempt in range(9):
        tm("switch-client", "-c", client, "-t", master)
        drain(.05)
        rows = tm("list-panes", "-t", master, "-F", "#{pane_id}|#{@csquad_panel}|#{pane_left}|#{pane_top}")
        panes = {parts[1]: parts for parts in (row.split("|") for row in rows.splitlines())}
        sidebar = panes["members"]
        tm("select-pane", "-t", panes[""][0])
        # A previous click from the other client must not steal this click.
        tm("set-option", "-p", "-t", sidebar[0], "@csquad_client", observer)
        x, y = int(sidebar[2]) + 5, int(sidebar[3]) + 6
        os.write(fd, f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline and sessions()[client] != worker:
            drain(.02)
        assert sessions()[client] == worker, (f"click {attempt} needed a second click; sessions={sessions()}; "
            f"origin={tm('show-options', '-pv', '-t', sidebar[0], '@csquad_client')}; "
            f"panel={tm('capture-pane', '-p', '-t', sidebar[0])}")
        assert sessions()[observer] == master, "mouse click switched another client"
        assert tm("show-options", "-pv", "-t", sidebar[0], "@csquad_client") == client
    os.write(fd, b"\x1b[1;3D\x1b[1;3C")
    drain(.2)
    assert sessions()[client] == worker, "Alt-arrow still switches members"
    print("PASS: first click, double/triple clicks, correct client, Alt-arrow passthrough")
finally:
    for fd, _, child in clients:
        child.terminate()
        try:
            child.wait(timeout=2)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        os.close(fd)
