"""Exercise single-click navigation through real tmux clients and pseudo-terminals."""
import fcntl
import os
import pty
import select
import signal
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
    return subprocess.check_output(["tmux", "-S", socket, *args], text=True).rstrip()


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
        x, y = int(sidebar[2]) + 5, int(sidebar[3]) + 15
        os.write(fd, f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())
        deadline = time.monotonic() + 2
        while time.monotonic() < deadline and sessions()[client] != worker:
            drain(.02)
        assert sessions()[client] == worker, (f"click {attempt} needed a second click; sessions={sessions()}; "
            f"origin={tm('show-options', '-pv', '-t', sidebar[0], '@csquad_client')}; "
            f"panel={tm('capture-pane', '-p', '-t', sidebar[0])}")
        assert sessions()[observer] == master, "mouse click switched another client"
        assert tm("show-options", "-pv", "-t", sidebar[0], "@csquad_client") == client
    # Both sessions keep their own sidebar process. Returning to a previously
    # visited session must highlight its owner, not the last outgoing target.
    for attempt in range(8):
        target = master if sessions()[client] == worker else worker
        index = 0 if target == master else 1
        rows = tm("list-panes", "-t", sessions()[client], "-F", "#{pane_id}|#{@csquad_panel}|#{pane_left}|#{pane_top}")
        sidebar = next(row.split("|") for row in rows.splitlines() if "|members|" in row)
        x, y = int(sidebar[2])+5, int(sidebar[3])+5+index*10
        os.write(fd, f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())
        deadline = time.monotonic()+2
        while time.monotonic()<deadline and sessions()[client]!=target:
            drain(.02)
        assert sessions()[client]==target, f"single-click round trip {attempt} failed: {sessions()} wanted {target}; pane={tm('capture-pane','-p','-t',sidebar[0])}"
        drain(.1)
        panels = tm("list-panes", "-t", target, "-F", "#{pane_id}|#{@csquad_panel}")
        pane = next(row.split("|")[0] for row in panels.splitlines() if row.endswith("|members"))
        name = "master" if target==master else "a"
        screen = tm("capture-pane", "-p", "-t", pane)
        assert any("▎" in line and line.replace("▎", "").replace("◆", "").strip()==name for line in screen.splitlines()), f"stale highlight after switching to {name}: {screen}"
    os.write(fd, b"\x1b[1;3D\x1b[1;3C")
    drain(.2)
    assert sessions()[client] == worker, "Alt-arrow still switches members"
    rows = tm("list-panes", "-t", worker, "-F", "#{pane_id}|#{@csquad_panel}|#{pane_left}|#{pane_top}")
    board = next(row.split("|") for row in rows.splitlines() if "|tasks|" in row)
    screen = tm("capture-pane", "-p", "-t", board[0]).splitlines()
    row = next(i for i,line in enumerate(screen) if "View details" in line)
    x,y=int(board[2])+5,int(board[3])+row+1
    os.write(fd,f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode()); drain(.3)
    detail=tm("capture-pane","-p","-t",board[0])
    assert "TASK DETAILS" in detail and "1/2" in detail, detail
    os.write(fd,b"\x1b")
    deadline=time.monotonic()+2
    while time.monotonic()<deadline and "View details" not in tm("capture-pane","-p","-t",board[0]):
        drain(.05)
    assert "View details" in tm("capture-pane","-p","-t",board[0]), "Escape failed to return to task cards"
    # The top-right close button collapses only the task panel.
    width=int(tm("display-message", "-p", "-t", board[0], "#{pane_width}"))
    x,y=int(board[2])+width-3,int(board[3])+2
    os.write(fd,f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())
    deadline=time.monotonic()+3
    while time.monotonic()<deadline and "tasks" in tm("list-panes","-t",worker,"-F","#{@csquad_panel}"):
        drain(.05)
    assert "tasks" not in tm("list-panes","-t",worker,"-F","#{@csquad_panel}"), "Close button failed"
    # Status buttons are right-aligned. Detach is 15 cells plus a one-cell gutter;
    # Tasks is the preceding 14-cell button.
    os.write(fd,b"\x1b[<0;155;40M\x1b[<0;155;40m")
    deadline=time.monotonic()+3
    while time.monotonic()<deadline and "tasks" not in tm("list-panes","-t",worker,"-F","#{@csquad_panel}"):
        drain(.05)
    assert "tasks" in tm("list-panes","-t",worker,"-F","#{@csquad_panel}"), "Tasks status button failed"
    # Real terminal resize events must trigger layout repair, without a
    # manual ui-layout call. Returning to a large window must not grow Header.
    for width, height in [(110, 25), (220, 80), (100, 22), (180, 40)]:
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        os.kill(clients[0][2].pid, signal.SIGWINCH)
        deadline=time.monotonic()+5
        while time.monotonic()<deadline:
            drain(.05)
            layout=tm("list-panes","-t",worker,"-F","#{@csquad_panel}:#{pane_width}:#{pane_height}")
            if f"header:{width}:3" in layout and "members:28:" in layout and (("tasks:" in layout)==(width>=150)):
                break
        else:
            raise AssertionError(f"resize to {width}x{height} did not settle: {layout}; clients={tm('list-clients','-F','#{client_name}:#{session_name}:#{client_width}:#{client_height}')}; window={tm('show-options','-w','-v','-t',worker,'window-size')}")
    os.write(fd,b"\x1b[<0;172;40M\x1b[<0;172;40m")
    deadline=time.monotonic()+3
    while time.monotonic()<deadline and client in sessions():
        time.sleep(.05)
    assert client not in sessions(), "Detach button failed"
    assert sessions()[observer]==master, "Detach removed another client"
    assert tm("has-session","-t",worker)=="", "Detach killed the team"
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
