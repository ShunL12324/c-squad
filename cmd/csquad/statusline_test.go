package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

var sgrRe = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func input(session string) string {
	b, _ := json.Marshal(map[string]string{"session_id": session, "transcript_path": "/nowhere/" + session + ".jsonl"})
	return string(b)
}

func statusline(t *testing.T, h *testutil.Home, session string, env ...string) []string {
	t.Helper()
	r := csq(t, h, opts{stdin: input(session), env: append([]string{"COLUMNS=100"}, env...)}, "statusline")
	if r.code != 0 {
		t.Fatalf("statusline exit %d: %s", r.code, r.stderr)
	}
	var out []string
	for _, l := range strings.Split(sgrRe.ReplaceAllString(r.stdout, ""), "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

func saveOwnLine(t *testing.T, h *testutil.Home, command string) {
	os.MkdirAll(h.Csquad, 0o755)
	b, _ := json.Marshal(map[string]any{"present": true, "statusLine": map[string]string{"type": "command", "command": command}})
	os.WriteFile(filepath.Join(h.Csquad, "statusline.json"), b, 0o644)
}

func TestStatuslineShowsOnlyThisSessionsTasks(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "mine")
	launchAs(t, h, "ffffffff-0000-4000-8000-000000000000", "theirs")
	setState(h, "1", func(s *testutil.Session) { s.State, s.Status = "blocked", "" })
	rows := statusline(t, h, sid)
	if len(rows) != 1 || !regexp.MustCompile(`^▎ ◆ T1 ✻ mine ··· +needs input +now$`).MatchString(rows[0]) {
		t.Errorf("%q", rows)
	}
	// A session that dispatched nothing never asks claude.
	before := h.Calls()
	if rows := statusline(t, h, "eeeeeeee-0000-4000-8000-000000000000"); len(rows) != 0 {
		t.Errorf("%q", rows)
	}
	if rows := statusline(t, h, ""); len(rows) != 0 {
		t.Errorf("no session: %q", rows)
	}
	if h.Calls() != before {
		t.Error("claude agents ran for a session without tasks")
	}
	// NO_COLOR: no colours, but the reverse-video badge stays.
	r := csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100", "NO_COLOR=1"}}, "statusline")
	if strings.Contains(r.stdout, "38;2;") || !strings.Contains(r.stdout, "\x1b[1;7m") || !strings.HasPrefix(r.stdout, "▌") {
		t.Errorf("NO_COLOR: %q", r.stdout)
	}
	r = csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100"}}, "statusline")
	if !strings.Contains(r.stdout, "48;2;230;166;20") {
		t.Errorf("colour: %q", r.stdout)
	}
}

func TestStatuslineOwnLineFirstAndFallback(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	flag := filepath.Join(h.Root, "own-fails")
	saveOwnLine(t, h, "test -e '"+flag+"' && exit 1; grep -o session_id | tr a-z A-Z")
	rows := statusline(t, h, sid)
	if len(rows) != 2 || rows[0] != "SESSION_ID" || !strings.Contains(rows[1], "T1 ✻ job") {
		t.Fatalf("%q", rows)
	}
	// Both the user's command and claude agents fail: the last good output stays.
	os.WriteFile(flag, nil, 0o644)
	rows = statusline(t, h, sid, "FAKE_AGENTS_FAIL=1")
	if len(rows) != 2 || rows[0] != "SESSION_ID" || !strings.Contains(rows[1], "T1 ✻ job") {
		t.Errorf("fallback: %q", rows)
	}
}

func TestStatuslinePartsRunInParallel(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	saveOwnLine(t, h, "sleep 1; echo mine")
	start := time.Now()
	rows := statusline(t, h, sid, "FAKE_AGENTS_SLEEP=1000")
	if took := time.Since(start); took > 1800*time.Millisecond {
		t.Errorf("took %v: not in parallel", took)
	}
	if len(rows) != 2 || rows[0] != "mine" {
		t.Errorf("%q", rows)
	}
}

func TestStatuslineAgeIsTimeInState(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "job")
	setState(h, "1", func(s *testutil.Session) { s.StartedAt = time.Now().Add(-2 * time.Hour).UnixMilli() })
	for i := 0; i < 2; i++ {
		if rows := statusline(t, h, sid); !strings.HasSuffix(rows[0], "working 2h00m") {
			t.Errorf("first seen working: since its start: %q", rows)
		}
	}
	setState(h, "1", func(s *testutil.Session) { s.State, s.Status = "done", "" })
	for i := 0; i < 2; i++ {
		if rows := statusline(t, h, sid); !regexp.MustCompile(`done +now$`).MatchString(rows[0]) {
			t.Errorf("changed state: since now: %q", rows)
		}
	}
}

func TestStatuslineCleansTitlesAndPrunesGoneTasks(t *testing.T) {
	h := testutil.NewHome(t)
	r := ok(t, csq(t, h, opts{stdin: "go", env: mine()}, "launch", "--title", "a\x1b[31mred\x07b", "--cwd", h.Root))
	_ = r
	plain := sgrRe.ReplaceAllString(csq(t, h, opts{stdin: input(sid), env: []string{"COLUMNS=100"}}, "statusline").stdout, "")
	if regexp.MustCompile(`[\x00-\x08\x0b-\x1f]`).MatchString(plain) || !strings.Contains(plain, "a [31mred b") {
		t.Errorf("%q", plain)
	}
	settings := filepath.Join(h.Csquad, "settings", "T1.json")
	if !exists(settings) {
		t.Fatal("no settings file")
	}
	h.SetSessions(nil) // removed with claude rm outside csquad
	statusline(t, h, sid)
	if exists(settings) {
		t.Error("a removed task's settings file (possibly a token) is left")
	}
}

// The usage cell: the status line reads the transcript's usage, keeps the
// reading with the earlier one and shows the total and the change; status
// shows the block.
func TestUsageInTheStatusLineAndStatus(t *testing.T) {
	h := testutil.NewHome(t)
	launchAs(t, h, sid, "busy job")
	short := getTask(t, h, 1).SessionID
	writeTranscript(t, h, "1",
		`{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":4,"cache_creation_input_tokens":51100,"cache_read_input_tokens":0,"output_tokens":300}}}`,
		`{"type":"assistant","message":{"id":"m1","usage":{"input_tokens":4,"cache_creation_input_tokens":51100,"cache_read_input_tokens":0,"output_tokens":622}}}`,
		`{"type":"assistant","message":{"id":"m2","usage":{"input_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":50500,"output_tokens":0}}}`)
	ms := time.Now().UnixMilli()
	seed := fmt.Sprintf(`{"session":%q,"samples":[{"at":%d,"fresh":0,"known":true},{"at":%d,"fresh":100,"known":true}]}`, short, ms-20_000, ms-10_000)
	os.MkdirAll(filepath.Join(h.Csquad, "cache"), 0o700)
	os.WriteFile(filepath.Join(h.Csquad, "cache", "T1.usage.json"), []byte(seed), 0o600)

	rows := statusline(t, h, sid)
	if len(rows) != 1 || !regexp.MustCompile(`working +52k +\+52k +now$`).MatchString(rows[0]) {
		t.Errorf("status line %q", rows)
	}
	b, _ := os.ReadFile(filepath.Join(h.Csquad, "cache", "T1.usage.json"))
	if !strings.Contains(string(b), `"fresh":51726`) {
		t.Errorf("the reading was not kept: %s", b)
	}
	// A reading less than 8 s later is not kept.
	statusline(t, h, sid)
	if b2, _ := os.ReadFile(filepath.Join(h.Csquad, "cache", "T1.usage.json")); string(b2) != string(b) {
		t.Errorf("kept a reading 0 s after the last: %s", b2)
	}

	out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout
	// The seconds since the refresh: 0, or more where processes start slowly.
	if !regexp.MustCompile(`usage: 51\.7k fresh · \+0 since the last refresh \(\d+ s\) · last activity `).MatchString(out) {
		t.Errorf("no usage line in\n%s", out)
	}
	for _, want := range []string{
		"       input 4 · cache write 51.1k · output 622  cache read 50.5k (not counted)",
		"       context 25% (50k of 200k) · status line +52k in ",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	var list []struct {
		Usage *struct {
			Fresh, CacheRead, ContextMax int64
			ChangeText                   string
			Change                       *int64
			SinceRefresh                 *int64
		} `json:"usage"`
	}
	r := ok(t, csq(t, h, opts{env: mine()}, "status", "--json"))
	if json.Unmarshal([]byte(r.stdout), &list) != nil || len(list) != 1 || list[0].Usage == nil || list[0].Usage.Fresh != 51726 ||
		list[0].Usage.CacheRead != 50500 || list[0].Usage.ContextMax != 200_000 || list[0].Usage.ChangeText != "+52k" || list[0].Usage.Change == nil || *list[0].Usage.Change != 51626 || list[0].Usage.SinceRefresh == nil {
		t.Errorf("status --json %s", r.stdout)
	}
	// Not working: the total, faint, without the change; no block in status.
	setState(h, "1", func(s *testutil.Session) { s.State, s.Status = "done", "" })
	if rows := statusline(t, h, sid); !regexp.MustCompile(`done +52k +now$`).MatchString(rows[0]) {
		t.Errorf("done: %q", rows)
	}
	if out := ok(t, csq(t, h, opts{env: mine()}, "status")).stdout; strings.Contains(out, "usage:") {
		t.Errorf("a task that is not working shows the block:\n%s", out)
	}
}
