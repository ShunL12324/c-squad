package app

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/statusline"
	"github.com/ShunL12324/c-squad/internal/store"
)

// usageEntry is the usage block of a working task in status --json.
type usageEntry struct {
	Known        bool   `json:"known"`
	Fresh        int64  `json:"fresh"`
	Input        int64  `json:"input"`
	CacheWrite   int64  `json:"cacheWrite"`
	Output       int64  `json:"output"`
	CacheRead    int64  `json:"cacheRead"` // shown, never counted
	Context      int64  `json:"context,omitempty"`
	ContextMax   int64  `json:"contextMax,omitempty"`
	LastActivity string `json:"lastActivity,omitempty"`
	// The status line's samples: the change since its newest one, when that
	// was taken, and the trail over them.
	SinceRefresh *int64 `json:"sinceRefresh,omitempty"`
	RefreshedAt  string `json:"refreshedAt,omitempty"`
	Trail        string `json:"trail,omitempty"`
	TrailSeconds int64  `json:"trailSeconds,omitempty"`
}

// working: the worker of a task in these states is at it; status shows its
// usage block.
func working(key string) bool {
	return key == engine.Working || key == store.Fixing || key == store.Merging
}

// contextMax is the task's context window: what the engine recorded, else
// for Claude 1M when its model asks for [1m] and 200k otherwise.
func contextMax(t store.Task, u engine.Usage) int64 {
	if u.ContextMax > 0 {
		return u.ContextMax
	}
	if t.Engine != "claude" {
		return 0
	}
	if strings.Contains(t.Model, "[1m]") || strings.Contains(strings.Join(t.EngineArgs, " "), "[1m]") {
		return 1_000_000
	}
	return 200_000
}

// usageOf reads a task's usage now and the status line's samples of it.
func (a *App) usageOf(ctx context.Context, t store.Task) (*usageEntry, []string) {
	eng, err := a.Engine(t.Engine)
	if err != nil || t.SessionID == "" {
		return nil, nil
	}
	ur, ok := eng.(engine.UsageReader)
	if !ok {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	u, err := ur.Usage(ctx, t.SessionID, a.usageState(t.ID))
	if err != nil {
		return nil, []string{"usage: unknown (" + firstLineOf(err.Error()) + ")"}
	}
	now := a.Now()
	e := &usageEntry{Known: u.Known, Fresh: u.Fresh(), Input: u.Input, CacheWrite: u.CacheWrite, Output: u.Output, CacheRead: u.CacheRead}
	var head []string
	if u.Known {
		head = append(head, statusline.TokensExact(u.Fresh())+" fresh")
	} else {
		head = append(head, "? (the session records no token usage)")
	}
	samples := statusline.LoadSamples(a.cacheDir(), t.ID, t.SessionID)
	if n := len(samples); n > 0 {
		newest := samples[n-1]
		at := time.UnixMilli(newest.At)
		e.RefreshedAt = at.UTC().Format(time.RFC3339)
		if u.Known && newest.Known {
			d := u.Fresh() - newest.Fresh
			e.SinceRefresh = &d
			sign := "+"
			if d < 0 {
				sign, d = "-", -d
			}
			head = append(head, fmt.Sprintf("%s%s since the last refresh (%s)", sign, statusline.TokensExact(d), seconds(now.Sub(at))))
		}
	}
	if !u.Activity.IsZero() {
		e.LastActivity = u.Activity.UTC().Format(time.RFC3339)
		head = append(head, "last activity "+ago(now.Sub(u.Activity))+" ago")
	}
	lines := []string{"usage: " + strings.Join(head, " · ")}
	if u.Known {
		lines = append(lines, fmt.Sprintf("       input %s · cache write %s · output %s  cache read %s (not counted)",
			statusline.TokensExact(u.Input), statusline.TokensExact(u.CacheWrite), statusline.TokensExact(u.Output), statusline.TokensExact(u.CacheRead)))
	}
	var tail []string
	if window := contextMax(t, u); u.Known && window > 0 && u.Context > 0 {
		e.Context, e.ContextMax = u.Context, window
		pct := int(math.Round(float64(u.Context) * 100 / float64(window)))
		tail = append(tail, fmt.Sprintf("context %d%% (%s of %s)", pct, statusline.Tokens(u.Context), statusline.Tokens(window)))
	}
	if len(samples) >= 2 {
		e.Trail = statusline.Trail(samples)
		span := time.Duration(samples[len(samples)-1].At-samples[0].At) * time.Millisecond
		e.TrailSeconds = int64(span.Seconds())
		tail = append(tail, fmt.Sprintf("last %s %s", seconds(span), e.Trail))
	}
	if len(tail) > 0 {
		lines = append(lines, "       "+strings.Join(tail, " · "))
	}
	return e, lines
}

// seconds is a short span in seconds ("20 s"), a longer one as ago does.
func seconds(d time.Duration) string {
	if d < 10*time.Minute {
		return fmt.Sprintf("%.0f s", math.Max(0, d.Seconds()))
	}
	return ago(d)
}
