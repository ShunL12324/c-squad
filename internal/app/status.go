package app

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/store"
)

// listed returns the unfinished tasks this session (or, with all, every
// session) dispatched, and their observed states.
func (a *App) listed(ctx context.Context, all bool) ([]store.Task, map[int]engine.Live, error) {
	db, err := a.Store()
	if err != nil {
		return nil, nil, err
	}
	f := store.Filter{}
	if !all {
		f.Parents = a.Owners()
	}
	tasks, err := db.List(ctx, f)
	if err != nil {
		return nil, nil, err
	}
	live, err := a.observe(ctx, tasks)
	tasks = a.clearGone(ctx, tasks, live)
	a.prune(ctx, gone(tasks, live))
	return tasks, live, err
}

// order is status's order: what needs the user first.
var order = []string{engine.NeedsInput, "needs_you", engine.Failed, Review, engine.Done, "ready", engine.Idle, engine.Working, engine.Stopped}

func rank(key string) int {
	for i, k := range order {
		if k == key {
			return i
		}
	}
	return len(order)
}

type statusEntry struct {
	ID        string      `json:"id"`
	Title     string      `json:"title"`
	Engine    string      `json:"engine"`
	Model     string      `json:"model,omitempty"`
	Phase     string      `json:"phase"`
	State     string      `json:"state"`
	Session   string      `json:"session"`
	Cwd       string      `json:"cwd"`
	InPlace   bool        `json:"inPlace,omitempty"`
	Parent    string      `json:"parentSession,omitempty"`
	StartedAt string      `json:"startedAt"`
	Latest    string      `json:"latest"`
	Attach    string      `json:"attach,omitempty"`
	Note      string      `json:"note,omitempty"`
	Hint      string      `json:"hint,omitempty"`
	After     []string    `json:"after,omitempty"`
	Check     string      `json:"check,omitempty"`
	Round     int         `json:"round,omitempty"`
	MaxRounds int         `json:"maxRounds,omitempty"`
	MergeInto string      `json:"mergeInto,omitempty"`
	Hold      bool        `json:"hold,omitempty"`     // --no-merge / csquad hold: never merged for the tasks queued after it
	Next      string      `json:"next,omitempty"`     // what to do for a task a hold blocks
	LastSeen  string      `json:"lastSeen,omitempty"` // the session's last seen state, when its engine did not answer now
	Usage     *usageEntry `json:"usage,omitempty"`    // working tasks
}

// Status lists this session's tasks (all: every task) with their state, age,
// session, directory and latest message.
func (a *App) Status(ctx context.Context, all, asJSON bool) error {
	tasks, live, err := a.listed(ctx, all)
	// A service that does not answer hides nothing: the tasks show from
	// the store, their sessions' states as last seen, unverified.
	down := notAnswering(err)
	if err != nil && len(live) == 0 && len(tasks) > 0 && down == nil {
		return err
	}
	if err != nil {
		if len(down) > 0 && !asJSON && len(tasks) > 0 {
			a.printf("warning: %v. The states below are not verified.\n\n", err)
		} else {
			a.warnf("csquad: %v\n", err)
		}
	}
	views := map[int]View{}
	q := a.queued(ctx)
	for _, t := range tasks {
		l, ok := live[t.ID]
		views[t.ID] = q.reviewView(view(t, l, ok), t, l, ok)
	}
	// unverified: the state a task's session was last seen in, for the
	// tasks of an engine that did not answer.
	unverified := map[int]string{}
	for _, t := range tasks {
		na := down[t.Engine]
		if na == nil || views[t.ID].Key != "unknown" {
			continue
		}
		text := "unknown: " + na.Service + " did not answer"
		if l, ok := na.Last[t.SessionID]; ok && l.State != "" {
			text += fmt.Sprintf("; last seen %s %s ago (unverified)", stateText(l.State), ago(a.Now().Sub(na.LastAt)))
			unverified[t.ID] = l.State
		}
		views[t.ID] = View{"unknown", text}
	}
	sort.SliceStable(tasks, func(i, j int) bool {
		ri, rj := rank(views[tasks[i].ID].Key), rank(views[tasks[j].ID].Key)
		if ri != rj {
			return ri < rj
		}
		return tasks[i].ID < tasks[j].ID
	})
	var entries []statusEntry
	for _, t := range tasks {
		started := time.UnixMilli(t.CreatedAt)
		if l := live[t.ID]; !l.StartedAt.IsZero() {
			started = l.StartedAt
		}
		latest := ""
		if eng, err := a.Engine(t.Engine); err == nil && t.SessionID != "" {
			if msgs, err := eng.Last(ctx, t.SessionID, 0); err == nil {
				latest = engine.LastAssistant(msgs)
			}
		}
		e := statusEntry{
			ID: t.Label(), Title: t.Title, Engine: t.Engine, Model: t.Model, Phase: t.Phase, State: views[t.ID].Text,
			Session: t.SessionID, Cwd: t.Cwd, InPlace: t.InPlace, Parent: t.ParentSession,
			StartedAt: started.UTC().Format(time.RFC3339), Latest: latest,
		}
		if eng, err := a.Engine(t.Engine); err == nil && t.SessionID != "" {
			e.Attach = strings.Join(eng.Takeover(t.SessionID), " ")
		}
		for _, id := range t.After {
			e.After = append(e.After, fmt.Sprintf("T%d", id))
		}
		if t.Phase == store.NeedsYou || t.Phase == store.Failed {
			e.Note = t.Note
		}
		if views[t.ID].Key == engine.Gone {
			e.Hint = fmt.Sprintf("the session was removed outside csquad; csquad cancel %s clears it (unmerged work is kept)", t.Label())
		}
		if t.CheckSpec != "" {
			e.Check, e.Round, e.MaxRounds = t.CheckSpec, t.Round, t.MaxRounds
		}
		e.MergeInto, e.Hold, e.Next = t.MergeInto, t.Hold, q.nextStep(t, views[t.ID])
		if s, ok := unverified[t.ID]; ok {
			e.LastSeen = s
		}
		var usage []string
		if working(views[t.ID].Key) {
			e.Usage, usage = a.usageOf(ctx, t)
		}
		entries = append(entries, e)
		if !asJSON {
			if latest == "" {
				latest = "(no reply yet)"
			}
			a.printf("## %s · %s\n", t.Label(), t.Title)
			a.printf("state: %s · started %s ago · session %s · %s\n", views[t.ID].Text, ago(a.Now().Sub(started)), t.SessionID, a.shortDir(t.Cwd))
			for _, l := range usage {
				a.printf("%s\n", l)
			}
			if e.Note != "" {
				a.printf("note: %s\n", e.Note)
			}
			if e.Hint != "" {
				a.printf("hint: %s\n", e.Hint)
			}
			if e.Next != "" {
				a.printf("next: %s\n", e.Next)
			}
			if p := strings.TrimPrefix(flowPlan(t)+a.implicitPlan(q, t), "\n  then: "); p != "" {
				a.printf("flow: %s\n", p)
			}
			// The message is indented, so its own "## " headings never read
			// as the next task's header.
			if cut := clip(latest, 1200); cut != latest {
				a.printf("latest:\n%s\n  (cut: csquad peek %s -n 1 shows all of it)\n\n", indent(cut), t.Label())
			} else {
				a.printf("latest:\n%s\n\n", indent(latest))
			}
		}
	}
	if asJSON {
		if entries == nil {
			entries = []statusEntry{}
		}
		b, _ := json.MarshalIndent(entries, "", "  ")
		a.printf("%s\n", b)
		return nil
	}
	if len(tasks) == 0 {
		if all || a.Owners() == nil {
			a.printf("No tasks.\n")
		} else {
			a.printf("No tasks launched by this session (status --all lists every task).\n")
		}
	}
	return nil
}

// Peek shows a task's recent conversation without tool traffic. The last
// assistant message shows in full; older messages are cut at 3000
// characters.
func (a *App) Peek(ctx context.Context, label string, n int, all bool) error {
	t, err := a.task(ctx, label, all, "peek")
	if err != nil {
		return err
	}
	if n <= 0 {
		n = 6
	}
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return err
	}
	live, oerr := a.observe(ctx, []store.Task{t})
	l, ok := live[t.ID]
	a.printf("%s · %s · session %s · %s\n\n", t.Name(), view(t, l, ok && oerr == nil).Text, t.SessionID, a.shortDir(t.Cwd))
	msgs, err := eng.Last(ctx, t.SessionID, n)
	if err != nil {
		return err
	}
	final := -1 // the last assistant message shows in full: it is the task's report
	for i, m := range msgs {
		if m.Role == "assistant" {
			final = i
		}
	}
	for i, m := range msgs {
		if m.Role == "tools" {
			var parts []string
			for _, c := range m.Tools {
				parts = append(parts, fmt.Sprintf("%s ×%d", c.Name, c.Count))
			}
			a.printf("[tools] %s\n\n", strings.Join(parts, ", "))
			continue
		}
		text := m.Text
		if i != final {
			text = clip(text, 3000)
		}
		a.printf("[%s]\n%s\n\n", m.Role, text)
	}
	return nil
}

// Send gives a task a follow-up instruction.
func (a *App) Send(ctx context.Context, label, text string, all bool) error {
	t, err := a.task(ctx, label, all, "message")
	if err != nil {
		return err
	}
	if store.Finished(t.Phase) {
		return fmt.Errorf("%s is %s", t.Label(), t.Phase)
	}
	if t.SessionID == "" {
		return fmt.Errorf("%s has no session yet", t.Label())
	}
	eng, err := a.Engine(t.Engine)
	if err != nil {
		return err
	}
	how, err := eng.Send(ctx, t.SessionID, text)
	if err != nil {
		return err
	}
	a.printf("sent to %s: %s\n", t.Name(), how)
	return nil
}

// indent indents every line of s by two spaces.
func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}
