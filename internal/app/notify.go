package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/flow"
	"github.com/ShunL12324/c-squad/internal/notify"
	"github.com/ShunL12324/c-squad/internal/store"
)

func (a *App) notifyLog() string { return filepath.Join(a.logsDir(), "notify.log") }

// noticeClass is the event a task's view shows, for the notifications; ""
// for a quiet state (working, checking, waiting, ...).
func noticeClass(t store.Task, v View) string {
	switch v.Key {
	case engine.NeedsInput:
		return notify.NeedsInput
	case "needs_you":
		return notify.NeedsYou
	case "ready":
		return notify.Ready
	case engine.Failed:
		// The flow moves a task with a check or a merge to the phase failed
		// itself; that move sends it.
		if t.Phase == store.Failed || !flow.Orchestrated(t) {
			return notify.Failed
		}
	case engine.Idle, engine.Done:
		// Only a task without a flow is finished when its turn ends.
		if t.Phase == store.Running && !flow.Orchestrated(t) {
			return notify.Done
		}
	}
	return ""
}

// phaseClass is the event of a phase csquad moved a task to; "" for none.
// Only a landing moves a task to done in the flow: merged.
func phaseClass(t store.Task) string {
	switch t.Phase {
	case store.Ready:
		return notify.Ready
	case store.NeedsYou:
		return notify.NeedsYou
	case store.Failed:
		return notify.Failed
	case store.Done:
		if t.MergeInto != "" {
			return notify.Merged
		}
	}
	return ""
}

// phaseEvent: the event comes from the task's phase, not from its live
// session.
func phaseEvent(t store.Task, class string) bool {
	switch class {
	case notify.Ready, notify.NeedsYou, notify.Merged:
		return true
	case notify.Failed:
		return t.Phase == store.Failed
	}
	return false
}

// observed records what an observation at `at` showed of tasks: it is
// called for every engine observation (observe).
func (a *App) observed(ctx context.Context, tasks []store.Task, live map[int]engine.Live, at time.Time) {
	for _, t := range tasks {
		l, ok := live[t.ID]
		if !ok || store.Finished(t.Phase) {
			continue
		}
		a.notice(ctx, t, noticeClass(t, view(t, l, true)), at)
	}
}

// moved records a phase change csquad wrote (t as stored).
func (a *App) moved(ctx context.Context, t store.Task) {
	if class := phaseClass(t); class != "" {
		a.notice(ctx, t, class, a.Now())
	}
}

// notice claims the event of t in the database and, when this process won
// the claim, starts a detached `csquad notify send`. It never blocks for
// long and never fails: the status line calls it too.
func (a *App) notice(ctx context.Context, t store.Task, class string, at time.Time) {
	mode := notify.Mode(a.Env, a.Home)
	if mode == notify.Off {
		return
	}
	phase := phaseEvent(t, class)
	if class == t.Notified && (!phase || t.NotifiedPhase == t.UpdatedAt) {
		return // nothing changed: no write
	}
	db, err := a.Store()
	if err != nil {
		return
	}
	send, err := db.ClaimNotice(ctx, t, class, at.UnixMilli(), phase)
	if err != nil {
		a.logNotify("%s: %v", t.Label(), err)
		return
	}
	if !send || !notify.Wants(mode, class) {
		return
	}
	if a.Env.Get("CSQUAD_NOTIFY_FILE") != "" {
		// The file backend (tests, debugging) is instant: no process that
		// outlives this one.
		if err := a.NotifySend(ctx, t.Label(), class); err != nil {
			a.logNotify("%s: %v", t.Label(), err)
		}
		return
	}
	if err := a.spawn(a.notifyLog(), "notify", "send", t.Label(), class); err != nil {
		a.logNotify("%s: %v", t.Label(), err)
	}
}

// logNotify appends a line to logs/notify.log.
func (a *App) logNotify(format string, args ...any) {
	if os.MkdirAll(a.logsDir(), 0o700) != nil {
		return
	}
	f, err := os.OpenFile(a.notifyLog(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(f, "%s %s\n", a.Now().Format(time.RFC3339), fmt.Sprintf(format, args...))
	f.Close()
}

// noticeText is the notification of an event: the task's name, the state
// and one detail.
func (a *App) noticeText(ctx context.Context, t store.Task, class string) notify.Message {
	detail := ""
	switch class {
	case notify.NeedsInput, notify.Done:
		if eng, err := a.Engine(t.Engine); err == nil && t.SessionID != "" {
			if msgs, err := eng.Last(ctx, t.SessionID, 0); err == nil {
				detail = engine.LastAssistant(msgs)
			}
		}
	case notify.NeedsYou:
		detail = t.Note
	case notify.Merged:
		var oc flow.Outcome
		_ = json.Unmarshal([]byte(t.Outcome), &oc)
		if oc.Target == "" {
			oc.Target = t.MergeInto
		}
		detail = "into " + oc.Target
	}
	return notify.Text(t.Name(), class, detail)
}

// NotifySend is the internal `csquad notify send T12 EVENT`, started
// detached by notice: it shows the notification. Its output goes to
// logs/notify.log.
func (a *App) NotifySend(ctx context.Context, label, class string) error {
	id, err := ParseLabel(label)
	if err != nil {
		return err
	}
	if !notify.Wants(notify.Mode(a.Env, a.Home), class) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, a.envMs("CSQUAD_NOTIFY_TIMEOUT_MS", 20*time.Second))
	defer cancel()
	db, err := a.Store()
	if err != nil {
		return err
	}
	t, err := db.Get(ctx, id)
	if err != nil {
		return err
	}
	m := a.noticeText(ctx, t, class)
	if class == notify.Done && m.Body == notify.Label(notify.Done) {
		// A session that has not answered yet is not done: an idle session
		// right after its start.
		a.logNotify("%s: no final message yet; nothing sent", t.Label())
		return nil
	}
	b, err := notify.Select(notify.Local(a.Env, a.Home))
	if errors.Is(err, notify.ErrNone) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := b.Send(ctx, m); err != nil {
		return fmt.Errorf("%s: %s: %v", t.Label(), b.Name(), err)
	}
	if _, file := b.(notify.File); !file {
		a.printf("%s %s: sent through %s\n", t.Label(), class, b.Name())
	}
	return nil
}

func modeText(mode string) string {
	switch mode {
	case notify.Off:
		return "off"
	case notify.Input:
		return "input (needs input and needs you only)"
	}
	return "on (needs input, needs you, ready, done, failed, merged)"
}

// Notify is `csquad notify [test|on|input|off]`.
func (a *App) Notify(ctx context.Context, args []string, title, body, wait string) error {
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "", "status":
		a.printf("Notifications: %s\n", modeText(notify.Mode(a.Env, a.Home)))
		if b, err := notify.Select(notify.Local(a.Env, a.Home)); err != nil {
			a.printf("Backend: none (%v)\n", err)
		} else {
			a.printf("Backend: %s\n", b.Name())
		}
		return nil
	case notify.On, notify.Input, notify.Off:
		if err := os.MkdirAll(a.Home, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(notify.ModeFile(a.Home), []byte(sub+"\n"), 0o644); err != nil {
			return err
		}
		a.printf("Notifications: %s\n", modeText(sub))
		if v := a.Env.Get("CSQUAD_NOTIFY"); v != "" && notify.ParseMode(v) != sub {
			a.printf("Note: CSQUAD_NOTIFY=%s is set here, and the variable wins over this setting.\n", v)
		}
		return nil
	case "test":
		return a.notifyTest(ctx)
	case "send":
		if len(args) != 3 {
			return errUsage
		}
		return a.NotifySend(ctx, args[1], args[2])
	case "post":
		ms, _ := strconv.Atoi(wait)
		if ms <= 0 {
			ms = 15000
		}
		return notify.Post(notify.Message{Title: title, Body: body}, time.Duration(ms)*time.Millisecond)
	}
	return errUsage
}

var errUsage = errors.New("usage")

// ErrUsage reports a usage error (main prints the command's usage).
func ErrUsage(err error) bool { return errors.Is(err, errUsage) }

func (a *App) notifyTest(ctx context.Context) error {
	sys := notify.Local(a.Env, a.Home)
	mode := notify.Mode(a.Env, a.Home)
	b, err := notify.Select(sys)
	if err != nil {
		a.printf("No notification backend: %v\n", err)
		return errSilent
	}
	switch tb := b.(type) {
	case notify.Toast:
		setup := notify.Setup{Sys: sys}
		if r, ok, err := setup.AppIDRegistrar(tb.WSL).Registered(); err != nil {
			a.printf("Warning: cannot read the app ID %s: %v\n", notify.AppID, err)
		} else if !ok {
			a.printf("Warning: the app ID %s is not registered, and Windows shows no toast without it; run csquad install.\n", notify.AppID)
		} else {
			a.printf("App ID %s: registered, icon %s\n", notify.AppID, r.Icon)
		}
	case notify.Bundle:
		tb.Wait = time.Minute // the user may answer macOS's permission request now
		b = tb
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	m := notify.Message{Title: "csquad · test", Body: "Notifications work. Mode: " + mode}
	if err := b.Send(ctx, m); err != nil {
		a.printf("The test notification failed through %s: %v\n", b.Name(), err)
		return errSilent
	}
	a.printf("Sent a test notification through %s.\n", b.Name())
	a.printf("Task notifications: %s\n", modeText(mode))
	return nil
}
