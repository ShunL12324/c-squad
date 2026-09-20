package teamui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func briefModel(width int, act Handler) model {
	return model{kind: "tasks", width: width, height: 60, act: act, briefs: map[string]briefFeedback{},
		data: Snapshot{Active: true, Tasks: []Task{{ID: "T1", Title: "Wire the panel", Owner: "a", State: "in progress"}}}}
}

// press resolves a card button the way Update does, so a test clicks the cells
// the panel actually paints rather than coordinates copied from the renderer.
func clickButton(t *testing.T, m model, action string) (tea.Model, tea.Cmd) {
	t.Helper()
	lines, hits := m.taskCards()
	for _, hit := range hits {
		for _, button := range hit.buttons {
			if button.action != action {
				continue
			}
			painted := ansi.Strip(lines[button.row])
			want := map[string]string{"details": strings.TrimSpace(detailsLabel), "brief": strings.TrimSpace(briefLabel)}[action]
			// The claimed cells must actually hold the label they trigger.
			if cells := []rune(painted); button.end <= len(cells) {
				if !strings.Contains(string(cells[button.start:button.end]), want) {
					t.Fatalf("%s button claims %q, which does not hold %q", action, string(cells[button.start:button.end]), want)
				}
			}
			return m.Update(tea.MouseMsg{X: button.start, Y: button.row + taskHeaderRows, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
		}
	}
	t.Fatalf("no %s button at width %d", action, m.width)
	return nil, nil
}

// Both layouts come from one function, so the painted button and the clickable
// region cannot drift apart at either width.
func TestCardButtonsRenderAndHitTestTogether(t *testing.T) {
	for _, tt := range []struct {
		name  string
		width int
		rows  int
	}{
		{"tasks panel", 40, 1},
		{"too narrow for one row", 26, 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got Action
			m := briefModel(tt.width, func(a Action) (string, error) { got = a; return "Asked master · M1", nil })
			rows, buttons := taskCardButtons(max(1, tt.width-6))
			if len(rows) != tt.rows || len(buttons) != 2 {
				t.Fatalf("width %d: %d row(s), %d button(s), want %d and 2", tt.width, len(rows), len(buttons), tt.rows)
			}

			next, cmd := clickButton(t, m, "details")
			if !next.(model).detail || cmd != nil {
				t.Fatal("details button did not open the detail view")
			}

			next, cmd = clickButton(t, m, "brief")
			if cmd == nil {
				t.Fatal("brief button raised no request")
			}
			if next.(model).detail {
				t.Fatal("brief button opened the detail view as well")
			}
			cmd()
			if got.Kind != "brief" || got.Task != "T1" {
				t.Fatalf("wrong action: %+v", got)
			}
		})
	}
}

// One physical double click arrives as several presses, because tmux forwards
// MouseDown, SecondClick, DoubleClick and TripleClick to the panel.
func TestRepeatedPressesRaiseOneRequest(t *testing.T) {
	var requests int
	m := briefModel(40, func(a Action) (string, error) { requests++; return "Asked master · M1", nil })
	var cmds []tea.Cmd
	for i := 0; i < 4; i++ {
		next, cmd := clickButton(t, m, "brief")
		m = next.(model)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	for _, cmd := range cmds {
		cmd()
	}
	if requests != 1 {
		t.Fatalf("four presses raised %d requests", requests)
	}
}

// A deliberate retry, after the first attempt has settled, is allowed.
func TestRetryAfterFailureIsAllowed(t *testing.T) {
	var requests int
	m := briefModel(40, func(a Action) (string, error) { requests++; return "Asked master · M1", nil })
	next, cmd := clickButton(t, m, "brief")
	m = next.(model)
	cmd()
	m.briefs["T1"] = briefFeedback{phase: briefFailed, text: "no master session", at: time.Now().Add(-time.Second)}
	if line := m.briefLine(m.data.Tasks[0], 34); !strings.Contains(line, "no master session") {
		t.Fatalf("failure is not shown on the card: %q", line)
	}
	next, cmd = clickButton(t, m, "brief")
	if cmd == nil {
		t.Fatal("retry was refused after the request had settled")
	}
	cmd()
	if requests != 2 {
		t.Fatalf("retry raised %d requests in total, want 2", requests)
	}
	_ = next
}

// The ledger, not this process, is what a respawned panel reads.
func TestCardShowsProjectedRequest(t *testing.T) {
	m := briefModel(40, nil)
	m.data.Tasks[0].Brief = Brief{MessageID: "M4", State: "sent"}
	card, _ := m.taskCard(0)
	if text := ansi.Strip(strings.Join(card, "\n")); !strings.Contains(text, "M4") {
		t.Fatalf("card does not show the outstanding request: %q", text)
	}
	m.data.Tasks[0].Brief = Brief{MessageID: "M4", State: "needs_attention", Error: "no ACK"}
	card, _ = m.taskCard(0)
	if text := ansi.Strip(strings.Join(card, "\n")); !strings.Contains(text, "no ACK") {
		t.Fatalf("card does not show the stalled request: %q", text)
	}
	m.data.Tasks[0].Brief = Brief{}
	card, _ = m.taskCard(0)
	if text := ansi.Strip(strings.Join(card, "\n")); strings.Contains(text, "Brief asked") {
		t.Fatalf("card shows a request nobody made: %q", text)
	}
}

// The detail header carries the same action, and the key works from anywhere.
func TestBriefFromDetailViewAndKeyboard(t *testing.T) {
	var got []Action
	m := briefModel(60, func(a Action) (string, error) { got = append(got, a); return "", nil })
	m.detail = true
	_, buttons := detailRow(m.width)
	var brief *cardButton
	for i := range buttons {
		if buttons[i].action == "brief" {
			brief = &buttons[i]
		}
	}
	if brief == nil {
		t.Fatal("detail header has no brief button")
	}
	next, cmd := m.Update(tea.MouseMsg{X: brief.start, Y: taskFilterRow, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	if cmd == nil {
		t.Fatal("detail brief button raised no request")
	}
	if !next.(model).detail {
		t.Fatal("asking for a brief closed the detail view")
	}
	cmd()

	keyed := briefModel(40, func(a Action) (string, error) { got = append(got, a); return "", nil })
	_, cmd = keyed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	if cmd == nil {
		t.Fatal("b did not raise a request")
	}
	cmd()

	// g offers master rather than switching the client behind the user's back.
	_, cmd = keyed.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd == nil {
		t.Fatal("g did not offer to open master")
	}
	cmd()
	if len(got) != 3 || got[2].Kind != "open" || got[2].Member != "master" {
		t.Fatalf("actions = %+v", got)
	}
}
