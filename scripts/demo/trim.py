#!/usr/bin/env python3
"""Speed up the waiting parts of a raw asciicast v3 recording.

Usage: trim.py RAW.cast MARKS OUT.cast

MARKS has lines "NAME EPOCH" written by demo.sh: "start" just before the
recording starts, then "fast" and "slow" switch the playback speed, and "end"
(or tmux detaching) cuts the rest. Fast parts play $DEMO_FAST (default 6)
times faster, so the tasks work in a few seconds and their rows still change
on screen.

agg draws a no-break space as a visible glyph, so it becomes a space.
"""
import json
import os
import sys

FAST = float(os.environ.get("DEMO_FAST", "6"))


def main(raw, marks, out):
    with open(raw) as f:
        header = json.loads(f.readline())
        events = [json.loads(line) for line in f if line.strip()]
    with open(marks) as f:
        ms = [(n, float(t)) for n, t in (line.split() for line in f if line.strip())]
    t0 = dict(ms)["start"]
    # Segments (from, rate) in seconds after the start.
    segs, end = [(0.0, 1.0)], None
    for name, t in ms:
        if name == "fast":
            segs.append((t - t0, 1 / FAST))
        elif name == "slow":
            segs.append((t - t0, 1.0))
        elif name == "end":
            end = t - t0

    def scale(t):
        out, i = 0.0, 0
        for i, (start, rate) in enumerate(segs):
            stop = segs[i + 1][0] if i + 1 < len(segs) else float("inf")
            if t <= start:
                break
            out += (min(t, stop) - start) * rate
        return out

    res, absolute, prev = [], 0.0, 0.0
    for ev in events:
        absolute += ev[0]
        if end is not None and absolute > end:
            break
        # tmux detaching: it clears the screen and leaves the alternate screen.
        if ev[1] == "o" and ("[detached" in ev[2] or "\x1b[?1049l" in ev[2]):
            break
        t = scale(absolute)
        if ev[1] == "o":
            ev[2] = ev[2].replace("\xa0", " ")
        res.append([round(t - prev, 6)] + ev[1:])
        prev = t
    with open(out, "w") as f:
        f.write(json.dumps(header) + "\n")
        for ev in res:
            f.write(json.dumps(ev, ensure_ascii=False) + "\n")


if __name__ == "__main__":
    main(*sys.argv[1:4])
