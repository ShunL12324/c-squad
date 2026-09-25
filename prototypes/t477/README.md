# T477 component prototype

This directory preserves the isolated terminal prototype used for the
component decision. The production side-panel port now lives in
`internal/teamui/side_panel.go` and the root module pins tview v0.42.0.

```sh
cd prototypes/t477
go run .                         # interactive, 32-column members + 40-column tasks
go run . -panel members          # member pane only
go run . -panel tasks            # task pane only
go run . -panel members -member-width 28
go run . -panel tasks -view done # includes cancelled tasks
go run . -snapshot -panel both   # fixed 72×46 ANSI render, no terminal needed
go run . -flow                   # Button handler callback trace
```

The captured 28×46 and 32×46 member renders are in `preview-members-28.ansi`
and `preview-members.ansi`; 40×46 task Active and Done renders are in
`preview-tasks.ansi` and `preview-tasks-done.ansi`. Text copies alongside them
make spacing easy to inspect. The snapshot is a tcell simulation render of the
same primitives as interactive mode. `preview-flow.txt` records Enter, click,
and disabled behavior through the actual tview Button handlers.

In interactive mode, Up/Down changes the focused member card; clicking a member
card also changes focus. Current session uses a blue border and darker filled
background; keyboard focus uses a distinct double-line green border. State and
task IDs are separate padded, colored chips, including Working, Idle, Blocked,
No task, and two task IDs on one member. The labels remain readable without
color. On the task side, Left/Right or clicking a stock Button tab changes
Active/Done; Cancelled appears under Done. Tab/Shift-Tab changes action Button
focus; Enter or a mouse click activates the focused/clicked action and updates
the line below the tabs. The third Active card demonstrates the library's
disabled Button state; it is a prototype example, not a product workflow.
`q` quits. The real `tview.Button` owns its Enter, click, disabled, focus, and
selected-callback behavior. The prototype only wires app-specific focus
traversal and callbacks. Each member surface composes `tview.TextView`, `Box`,
and `Flex`; tview does **not** supply a stock Card control. The tmux header
height is outside this prototype and T477's implementation scope.

## Production preview

`current-members-28.{ansi,txt}`, `current-members-32.{ansi,txt}`,
`current-members-28x12.{ansi,txt}`, `current-tasks-40x12.{ansi,txt}`,
`current-tasks-40.{ansi,txt}`, `current-tasks-done-40.{ansi,txt}`, and
`current-task-detail-40.{ansi,txt}` are
captures of the production `teamui.Run` renderer in isolated tmux panes at
28×12, 40×12, 28×46, 32×46, and 40×46. Their labels and
statuses use selected current team ledger values on 2026-09-26: Master, navigation-dev,
observation-dev, active T477/T480, done T470, and cancelled T302. The root
module was compiled into a temporary preview binary; no live team pane or
member process was modified. ANSI captures retain background and chip color;
text captures expose spacing. The product uses one tview event loop per side
pane; the header remains a separate Bubble Tea read-only pane. The production
task button remains enabled for every state, including Blocked and Review.

## Component choice

| Option | Provenance and fit | Migration cost |
| --- | --- | --- |
| [tview v0.42.0](https://github.com/rivo/tview/releases/tag/v0.42.0) | [Button](https://github.com/rivo/tview/blob/v0.42.0/button.go) handles Enter, focus style, and left-click; [Flex](https://github.com/rivo/tview/blob/v0.42.0/flex.go) sizes children. Its [module](https://github.com/rivo/tview/blob/v0.42.0/go.mod) targets Go 1.18 and uses tcell v2. Repository activity continued in August 2026. | Medium/high: tview owns a tcell event loop and screen. The two `teamui` side-pane programs, their Bubble Tea Update/View models, mouse handling, rendering fixtures, and lifecycle need a coordinated port. A tcell simulation adapter inside Bubble Tea would duplicate event routing and undermine the stock Button semantics. |
| [Bubbles v0.21](https://github.com/charmbracelet/bubbles/tree/v0.21.0) and [Huh](https://github.com/charmbracelet/huh) | Existing Bubbles list/viewport are retained in the product, but Bubbles has no standalone Button or Card. Huh's `Confirm` is a form field rather than a reusable task action button. | Low for visual surfaces; still needs a bespoke button and pointer/focus mechanics, contrary to this task's component requirement. |
| [Mate](https://github.com/muralx/mate) | Offers Bubble Tea Button and Card with routed mouse events. Its current [module](https://github.com/muralx/mate/blob/main/go.mod) requires Go 1.26.1 and Bubbles 1.0, while this project targets Go 1.25 and Bubbles 0.21. The project is young and has no stable release. | Potentially medium, but first requires a toolchain and component stack upgrade; poor release risk for this iteration. |

**Recommendation:** choose a scoped tview port of the two side-pane UIs if a real
library Button is mandatory. Keep the tmux pane layout, snapshot data model, and
action callback boundary; replace only the teamui rendering/input loop. Use
`TextView`/`Box`/`Flex` for visible member surfaces and `Button` for task actions.
The prototype makes the desired look concrete, but is intentionally not a
production migration. Integration should begin only after Master accepts this
framework tradeoff and checks the native keyboard/mouse contract.
