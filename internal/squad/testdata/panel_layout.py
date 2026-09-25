"""Assert the outer panel layout stays stable when a client enters a member session.

Modes:
  stable <socket> <master> <newbie>  click the new member's card and assert the
                                     layout never reflows, then resize the client
                                     and assert automatic sizing still follows.
  hold   <socket> <master>           attach a client and hold it so the caller can
                                     inspect team geometry while one is attached.
"""
import errno, fcntl, os, pty, select, signal, struct, subprocess, sys, termios, time

mode, socket, master = sys.argv[1:4]
env = dict(os.environ, TERM="xterm-256color")
for key in ("TMUX", "TMUX_PANE"):
    env.pop(key, None)
clients = []


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


def layout(session):
    rows = tm("list-panes", "-t", session, "-F", "#{@csquad_panel}:#{pane_width}x#{pane_height}")
    return " ".join(sorted(row if row.split(":")[0] else "engine" + row for row in rows.splitlines()))


def sessions():
    return dict(row.split("|") for row in tm("list-clients", "-F", "#{client_name}|#{session_name}").splitlines())


def card(session, member):
    """Pointer coordinates of a member's sidebar card, once it has rendered."""
    name = member.split("-")[-1]
    deadline = time.monotonic() + 5
    while True:
        rows = tm("list-panes", "-t", session, "-F", "#{pane_id}|#{@csquad_panel}|#{pane_left}|#{pane_top}")
        panel = next((row.split("|") for row in rows.splitlines() if "|members|" in row), None)
        if panel:
            screen = tm("capture-pane", "-p", "-t", panel[0]).splitlines()
            row = next((i for i, text in enumerate(screen) if text.strip().endswith(name)), None)
            if row is not None:
                # tmux mouse coordinates are 1-based; capture rows are not.
                return int(panel[2]) + 5, int(panel[3]) + row + 1
        if time.monotonic() > deadline:
            raise AssertionError(f"{member} never appeared in the {session} sidebar")
        drain(.1)


def click_and_watch(fd, client, target, seconds=4):
    """Click the target's card and record every distinct layout it renders.

    A layout that reflows and is repaired a moment later shows up here as more
    than one state, which is exactly the defect under test, so sampling has to
    start the instant the client lands rather than after a settling delay.
    """
    seen, clicked, last = [], 0, 0.0
    deadline = time.monotonic() + seconds
    while time.monotonic() < deadline:
        current = sessions().get(client)
        if current != target:
            if not seen and clicked < 3 and time.monotonic() - last > 1.5:
                x, y = card(current, target)
                os.write(fd, f"\x1b[<0;{x};{y}M\x1b[<0;{x};{y}m".encode())
                clicked, last = clicked + 1, time.monotonic()
            drain(.01)
            continue
        state = layout(target)
        if not seen or seen[-1] != state:
            seen.append(state)
        drain(.01)
    assert seen, f"click never switched the client to {target}: {sessions()}"
    return seen


def attach(width, height):
    fd, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
    child = subprocess.Popen(["tmux", "-S", socket, "attach", "-t", master],
                             stdin=slave, stdout=slave, stderr=slave, env=env, start_new_session=True)
    os.close(slave)
    clients.append((fd, child))
    drain(1.5)
    return fd


try:
    if mode == "controlled":
        fd = attach(180, 40)
        print("attached", flush=True)
        for command in sys.stdin:
            width = int(command.strip())
            fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, width, 0, 0))
            os.kill(clients[0][1].pid, signal.SIGWINCH)
            drain(.3)
            print("resized", flush=True)
        sys.exit(0)

    if mode == "hold":
        attach(180, 40)
        print("attached", flush=True)
        drain(float(sys.argv[4]))
        sys.exit(0)

    if mode == "native":
        fd = attach(280, 79)
        client = list(sessions())[0]
        timings = []
        for _ in range(4):
            for keys, target in ((b"\x1b[1;3B", "layout-a"), (b"\x1b[1;3A", master)):
                started = time.monotonic()
                os.write(fd, keys)
                while sessions().get(client) != target:
                    assert time.monotonic() - started < 3, "keyboard switch timed out"
                    drain(.002)
                timings.append((time.monotonic()-started)*1000)
                drain(.1)
        print("keyboard switch milliseconds:", timings, flush=True)
        def drag_members():
            rows = [row.split("|") for row in tm("list-panes", "-t", master,
                    "-F", "#{pane_id}|#{@csquad_panel}|#{pane_right}|#{pane_top}|#{pane_width}").splitlines()]
            members = next(row for row in rows if row[1] == "members")
            x, y = int(members[2])+2, int(members[3])+8
            os.write(fd, f"\x1b[<0;{x};{y}M".encode())
            for offset in range(1, 13):
                os.write(fd, f"\x1b[<32;{x+offset};{y}M".encode())
                drain(.005)
            os.write(fd, f"\x1b[<0;{x+12};{y}m".encode())
            # Writing to the PTY does not mean tmux has processed mouse release.
            # Wait for the release binding to finish saving geometry.
            deadline = time.monotonic() + 1
            while tm("show-options", "-wqv", "-t", master, "@csquad_dragging"):
                assert time.monotonic() < deadline, "native border release was not processed"
                drain(.005)
            actual = int(tm("display-message", "-p", "-t", members[0], "#{pane_width}"))
            print("native drag actual/pref:", actual, tm("show-options", "-wv", "-t", master, "@csquad_size_members"), flush=True)
            assert actual > int(members[4]), "native border drag did not resize"
            saved = int(tm("show-options", "-wv", "-t", master, "@csquad_size_members"))
            assert actual == saved, f"native drag release left stale preference: {actual} != {saved}"

        drag_members()
        immediate = layout(master)
        for target in ("layout-b", master):
            assert click_and_watch(fd, client, target, seconds=1) == [immediate]
        drag_members()
        # Expand before navigation has any chance to sample the final drag.
        dragged = tm("show-options", "-wv", "-t", master, "@csquad_size_members")
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 79, 300, 0, 0))
        os.kill(clients[0][1].pid, signal.SIGWINCH)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            geometry = layout(master)
            if ("header:300x" in geometry and "members:" + dragged + "x" in geometry
                    and tm("show-options", "-wv", "-t", master, "@csquad_geometry") == "300 77"):
                break
            drain(.02)
        else:
            raise AssertionError(f"expansion lost native drag {dragged}: {geometry}")
        expected = layout(master)
        for target in ("layout-b", master, "layout-a", master):
            observed = click_and_watch(fd, client, target, seconds=1)
            assert observed == [expected], f"native drag lost on {target}: {expected} -> {observed}"
        drag_members()
        expected = layout(master)
        os.write(fd, b"\x1b[1;3B")
        deadline = time.monotonic()+3
        while sessions().get(client) != "layout-a" and time.monotonic()<deadline:
            drain(.01)
        assert sessions().get(client) == "layout-a", "keyboard switch failed"
        assert layout("layout-a") == expected, "keyboard lost native drag"
        print("PASS native drag immediate switch and round trips", flush=True)
        sys.exit(0)

    newbie = sys.argv[4]
    fd = attach(180, 40)
    client = list(sessions())[0]
    initial_panes = dict(row.split("|") for row in tm("list-panes", "-t", master,
                         "-F", "#{@csquad_panel}|#{pane_id}").splitlines())
    for role, axis, size in (("members", "-x", "32"), ("tasks", "-x", "43"), ("header", "-y", "4")):
        tm("resize-pane", "-t", initial_panes[role], axis, size)
    initial_custom = layout(master)
    before = layout(newbie)
    print("new member layout before the click:", before)

    seen = click_and_watch(fd, client, newbie)
    for state in seen:
        print("  observed:", state)
    assert len(seen) == 1, f"outer layout reflowed on the first click: {seen}"
    assert seen == [initial_custom], f"first visit lost custom geometry: {initial_custom} -> {seen}"

    # Manually adjusted chrome must follow existing and first-visited sessions,
    # including round trips. These are separate tmux windows, not shared panes.
    panes = dict(row.split("|") for row in tm("list-panes", "-t", newbie,
                 "-F", "#{@csquad_panel}|#{pane_id}").splitlines())
    tm("resize-pane", "-t", panes["members"], "-x", "35")
    tm("resize-pane", "-t", panes["tasks"], "-x", "47")
    tm("resize-pane", "-t", panes["header"], "-y", "5")
    # Resize immediately after dragging, before any member switch has had an
    # opportunity to sample the dimensions. Expansion needs no pane shrink.
    for width in (200, 180):
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, width, 0, 0))
        os.kill(clients[0][1].pid, signal.SIGWINCH)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            drain(.05)
            geometry = layout(newbie)
            if f"header:{width}x5" in geometry and "members:35x" in geometry and "tasks:47x" in geometry:
                break
        else:
            raise AssertionError(f"resize lost freshly dragged dimensions: {geometry}")
    custom = layout(newbie)
    for target in (master, master.rsplit("-", 1)[0] + "-a", newbie, master, newbie):
        moved = click_and_watch(fd, client, target, seconds=2)
        assert moved == [custom], f"manual dimensions lost switching to {target}: {custom} -> {moved}"

    tm("resize-pane", "-t", panes["members"], "-x", "36")
    keyboard_custom = layout(newbie)
    for keys, target in ((b"\x020", master), (b"\x022", newbie),
                         (b"\x1b[1;3A", master.rsplit("-", 1)[0] + "-a"), (b"\x1b[1;3B", newbie)):
        os.write(fd, keys)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline and sessions().get(client) != target:
            drain(.02)
        assert sessions().get(client) == target, f"keyboard switch failed: {sessions()}"
        assert layout(target) == keyboard_custom, f"keyboard switch lost dimensions: {layout(target)}"
        drain(.2)
    tm("resize-pane", "-t", panes["members"], "-x", "35")

    # fitSession pins the window with resize-window; automatic sizing has to be
    # handed back or later client resizes would stop reaching this session.
    assert tm("show-options", "-w", "-v", "-t", newbie, "window-size") == "latest", "window stayed pinned"
    for width, height in ((120, 30), (180, 40)):
        fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", height, width, 0, 0))
        os.kill(clients[0][1].pid, signal.SIGWINCH)
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            drain(.05)
            if tm("display-message", "-p", "-t", newbie, "#{window_width}") == str(width):
                break
        else:
            raise AssertionError(f"resize to {width} never reached the session: {layout(newbie)}")
    # The same click has to stay stable on a narrow client, where the task panel
    # is not shown at all and only the sidebar shares the window.
    # Narrow means "too narrow for the task panel"; keep the height so the
    # sidebar still lists enough members to click one.
    fcntl.ioctl(fd, termios.TIOCSWINSZ, struct.pack("HHHH", 40, 100, 0, 0))
    os.kill(clients[0][1].pid, signal.SIGWINCH)
    drain(1)
    tm("switch-client", "-c", client, "-t", master)
    deadline = time.monotonic() + 5
    while time.monotonic() < deadline and tm("display-message", "-p", "-t", master, "#{window_width}") != "100":
        drain(.05)
    narrow = click_and_watch(fd, client, newbie)
    for state in narrow:
        print("  narrow observed:", state)
    assert "members:36x" in narrow[0] and "header:100x5" in narrow[0], f"narrow switch reset custom dimensions: {narrow}"
    assert len(narrow) == 1, f"outer layout reflowed on a narrow client: {narrow}"

    print("PASS: no layout jump on the first click, automatic resizing preserved")
finally:
    for fd, child in clients:
        child.terminate()
        try:
            child.wait(timeout=2)
        except subprocess.TimeoutExpired:
            child.kill()
            child.wait()
        os.close(fd)
