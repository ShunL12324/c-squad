# README animation

`demo.sh` records `docs/demo/demo.cast` and renders `docs/images/demo.gif`, the animation at the
top of the README. Make it again after a change to the status line or the skills.

## Requirements

- tmux, python3, jq, go, curl, and `claude` on `PATH`.
- [asciinema](https://github.com/asciinema/asciinema) 3 and [agg](https://github.com/asciinema/agg).
  To use binaries that are not on `PATH`, set `ASCIINEMA` and `AGG`.
- The fonts DejaVu Sans Mono and Noto Sans Mono CJK. `render` downloads Noto Sans Symbols 2.
- A provider in `ANTHROPIC_BASE_URL` and `ANTHROPIC_AUTH_TOKEN`. The script forwards them to the
  demo environment. It does not print them.

## Steps

```sh
scripts/demo/demo.sh setup    # temp HOME, Claude Code config, csquad install, ~/demo/todo-cli
scripts/demo/demo.sh record   # about 2 minutes: drives claude in tmux and records it
scripts/demo/demo.sh render   # trims the recording, writes the .cast and the .gif
scripts/demo/demo.sh clean    # cancels the tasks, removes the sessions and the temp dir
```

All steps use `DEMO_ROOT` (default `/tmp/csquad-demo`) as the temp directory. The tool calls in
the recording show this path, so do not use a path with your user name in it. In that directory,
`home` is the `HOME` of the demo, so the demo project shows as `~/demo/todo-cli`. Your own
`~/.claude`, `~/.csquad` and tmux server are not used. `demo.sh run CMD` runs a command in the
demo environment, for example `scripts/demo/demo.sh run csquad status --all`.

## What the script does

1. `setup` builds csquad from this checkout and installs it into the temp environment. It accepts
   onboarding and trust in the temp `.claude.json`, sets a status line with the model and the
   directory only, and makes a small Go todo CLI with three commits. The tasks use `haiku`.
2. `record` starts `claude --model sonnet` in tmux at 120x32 and records it with asciinema. It
   types the requests at a human speed and waits for each status line row on the screen:
   - "create two tasks: …": T1 and T2 start.
   - "after T1, a task to add a CHANGELOG entry for `--done`": T3 shows `waiting T1`.
   - T1 becomes `done`.
   - "finish T1": Claude merges T1, its row goes, and T3 starts.
3. `render` runs `trim.py`. The script plays the waits 4 times faster (`DEMO_FAST`) and cuts the
   end. Then agg makes the GIF.

Look at some frames before you commit, for example with
`ffmpeg -i docs/images/demo.gif -vf fps=1/3 /tmp/f%02d.png`. Make sure that the glyphs show and
that no token, user name or host name shows.
