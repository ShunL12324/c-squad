# T541 side-panel preview

These previews render the **production `sidePanel`** on a `tcell.SimulationScreen`. The build-tagged renderer writes the terminal cells as fixed-width text, ANSI color, and SVG; `render_images.py` rasterizes the SVG for review. The preview path contains no separate card layout or input widget.

## Compare at the actual pane sizes

| View | Rendered image | Terminal capture |
| --- | --- | --- |
| Member panel, screenshot-style data, 28×46 | [PNG](members-screenshot-28x46.png) | [ANSI](members-screenshot-28x46.ansi) |
| Task panel, screenshot-style Done tasks, 40×46 | [PNG](tasks-screenshot-40x46.png) | [ANSI](tasks-screenshot-40x46.ansi) |
| Member panel, active team, 28×46 | [PNG](members-28x46.png) | [ANSI](members-28x46.ansi) |
| Member panel, active team, 32×46 | [PNG](members-32x46.png) | [ANSI](members-32x46.ansi) |
| Member panel, short, 28×12 | [PNG](members-28x12.png) | [ANSI](members-28x12.ansi) |
| Active task panel, Working / Review / Blocked, 40×46 | [PNG](tasks-active-40x46.png) | [ANSI](tasks-active-40x46.ansi) |
| Active task panel, short, 40×12 | [PNG](tasks-active-40x12.png) | [ANSI](tasks-active-40x12.ansi) |
| Done / Cancelled task panel, 40×46 | [PNG](tasks-done-40x46.png) | [ANSI](tasks-done-40x46.ansi) |
| Full task detail, 40×46 | [PNG](task-detail-40x46.png) | [ANSI](task-detail-40x46.ansi) |

The [Review](tasks-review-40x46.png) and [Blocked](tasks-blocked-40x46.png) images show selection moving to later tasks. The screenshot-style examples use the same three people, the two visible long release titles, and the screenshot's **28 Done task count**; the 26 unseen entries are illustrative. The active examples add multiple assignments, no task, and Working, Idle, Blocked, Review, Done, and Cancelled states.

Member name and task owner accents use the existing persisted tmux member color through the snapshot. The card surface signals current session, keyboard focus, and their overlap without a dot or outline. Status and task IDs use small semantic chips; `No task` is plain muted text. Task lists show a bounded title and one progress or completion line; the existing detail view keeps the full content, milestones, and latest update. Buttons remain actual `tview.Button` controls. At 40×46, compact cards fit three tasks without reserved blank rows, including the first page of the screenshot's 28 Done tasks. When fewer tasks exist, the remaining panel space stays neutral instead of stretching cards. Page numbers appear only when there is another page. The existing bounded page/wheel layout keeps Master pinned and Button focus predictable; replacing it with a new scrolling interaction layer would add custom control mechanics for no additional visible row at this width.

Regenerate from the repository root:

```sh
go run -tags t541preview ./prototypes/t541
python3 prototypes/t541/render_images.py
```

The second command needs local Chromium and FFmpeg. It only converts the generated SVG to PNG; terminal rendering does not depend on either tool. Validation tests are pending Master’s scheduled integrated batch.
