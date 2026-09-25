# T477 component prototype

This is an isolated terminal prototype. It leaves the shipped Bubble Tea panels
and the root `go.mod` unchanged.

```sh
cd prototypes/t477
go run .                         # interactive, 32-column members + 40-column tasks
go run . -panel members          # member pane only
go run . -panel tasks            # task pane only
go run . -snapshot -panel both   # fixed 72×46 ANSI render, no terminal needed
```

The captured 32×46 and 40×46 renders are in `preview-members.ansi` and
`preview-tasks.ansi`; text copies alongside them make spacing easy to inspect.
The snapshot is a tcell simulation render of the same primitives as interactive
mode. It does not simulate keyboard or pointer input.

In interactive mode, Up/Down changes the focused member card; clicking a member
card also changes focus. The `●` marks the current session independently of the
double-line focus border. On the task side, Tab/Shift-Tab changes button focus;
Enter or a mouse click activates the focused/clicked button and updates the
status line. `q` quits. The real `tview.Button` owns its Enter, click, disabled,
focus, and selected-callback behavior. The prototype only wires app-specific
focus traversal and callbacks. Each member surface composes `tview.TextView`,
`Box`, and `Flex`; tview does **not** supply a stock Card control.

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
