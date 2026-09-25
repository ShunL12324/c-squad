"""Alt+Up / Alt+Down switch member sessions in one keypress, from any pane.

The sidebar pane is created with split-window -d and every navigation selects the
engine pane, so the panel almost never holds focus. These bindings live in the
team's tmux root table, which is consumed before the pane sees the key.
"""
import errno, fcntl, os, pty, select, signal, struct, subprocess, sys, termios, time

socket, master, first, last = sys.argv[1:5]
height = int(sys.argv[5]) if len(sys.argv) > 5 else 40
env = dict(os.environ, TERM="xterm-256color")
for key in ("TMUX", "TMUX_PANE"):
    env.pop(key, None)
clients = []

ALT_UP, ALT_DOWN = b"\x1b[1;3A", b"\x1b[1;3B"
META_UP = b"\x1b[1;9A"


def tm(*args):
    return subprocess.check_output(["tmux", "-S", socket, *args], text=True).rstrip()


def drain(seconds):
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        ready, _, _ = select.select([c[0] for c in clients], [], [], .01)
        for fd in ready:
            try:
                os.read(fd, 65536)
            except OSError as error:
                if error.errno != errno.EIO:
                    raise


def sessions():
    return dict(row.split("|") for row in tm("list-clients", "-F", "#{client_name}|#{session_name}").splitlines())


def press(fd, client, keys, want):
    os.write(fd, keys)
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline and sessions().get(client) != want:
        drain(.02)
    assert sessions().get(client) == want, f"{keys!r} did not reach {want}: {sessions()}"


def panel(session, view):
    rows = tm("list-panes", "-t", session, "-F", "#{pane_id}|#{@csquad_panel}")
    return next(row.split("|")[0] for row in rows.splitlines() if row.endswith("|" + view))


def owner_highlighted(session, member):
    """The session's own sidebar marks its owner with the │ stripe."""
    deadline = time.monotonic() + 3
    while time.monotonic() < deadline:
        screen = tm("capture-pane", "-p", "-t", panel(session, "members"))
        for row in screen.splitlines():
            # The right gutter may show a scrollbar independently of the
            # owner's stripe on the left. It is not part of the member name.
            title = row.rstrip().removesuffix("│").removesuffix("┃").strip()
            if title.startswith("│") and title[1:].strip().removeprefix("◆").strip() == member:
                return True
        drain(.05)
    print(f"Missing owner highlight for {member!r} in {session}:\n{screen}", flush=True)
    return False


try:
    fd, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, 180, 0, 0))
    child = subprocess.Popen(["tmux", "-S", socket, "attach", "-t", master],
                             stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
    os.close(slave)
    clients.append((fd, child))
    drain(1.5)
    client = list(sessions())[0]

    # The engine pane is focused on attach; that is the whole point of the fix.
    engine = tm("display-message", "-p", "-t", master, "#{pane_id}")
    assert engine == panel(master, ""), f"expected the engine pane to be active, got {engine}"

    press(fd, client, ALT_DOWN, first)
    assert owner_highlighted(first, first.split("-")[-1]), "sidebar highlight did not follow the switch"
    press(fd, client, ALT_DOWN, last)
    # Wrap-around: the list is cyclic, like the C-b 0-9 shortcuts.
    press(fd, client, ALT_DOWN, master)
    press(fd, client, ALT_UP, last)
    assert owner_highlighted(last, last.split("-")[-1]), "sidebar highlight did not follow the reverse switch"

    # Focus-independence: the same keys work while the sidebar has focus.
    tm("select-pane", "-t", panel(last, "members"))
    press(fd, client, ALT_UP, first)

    # tmux collapses the Alt and Meta encodings into one M- namespace.
    press(fd, client, META_UP, master)

    # Narrow clients drop the panels entirely; switching is a key table binding
    # and must not depend on a sidebar being rendered.
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 24, 80, 0, 0))
    os.kill(clients[0][1].pid, signal.SIGWINCH)
    drain(.5)
    press(fd, client, ALT_DOWN, first)
    press(fd, client, ALT_UP, master)
    print("PASS: single-keypress member switching, wrap-around, highlight in sync, any pane")
finally:
    for fd, child in clients:
        child.terminate()
        try:
            child.wait(timeout=2)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        os.close(fd)
