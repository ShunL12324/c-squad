package claude

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/testutil"
)

func assistant(id string, in, write, read, out int) string {
	return fmt.Sprintf(`{"type":"assistant","message":{"id":%q,"usage":{"input_tokens":%d,"cache_creation_input_tokens":%d,"cache_read_input_tokens":%d,"output_tokens":%d}}}`, id, in, write, read, out)
}

func TestUsageCountsEachMessageOnceAndReadsIncrementally(t *testing.T) {
	h := testutil.NewHome(t)
	e := newEngine(h)
	sid := "0f0f0f0f-1111-4222-8333-444455556666"
	dir := filepath.Join(h.Claude, "projects", "-repo")
	os.MkdirAll(dir, 0o755)
	file := filepath.Join(dir, sid+".jsonl")
	state := filepath.Join(h.Csquad, "cache", "T1.read.json")
	write := func(lines ...string) {
		os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	}
	lines := []string{
		`{"type":"user","message":{"content":"go"}}`,
		assistant("m1", 4, 1000, 0, 10),
		assistant("m1", 4, 1000, 0, 200), // the same message, a later block: last wins
		`{"type":"user","message":{"content":[{"type":"tool_result"}]}}`,
		assistant("m2", 2, 300, 1000, 50),
	}
	write(lines...)
	u, err := e.Usage(context.Background(), sid, state)
	if err != nil || !u.Known || u.Input != 6 || u.CacheWrite != 1300 || u.Output != 250 || u.CacheRead != 1000 || u.Fresh() != 1556 || u.Context != 1302 {
		t.Fatalf("%+v %v", u, err)
	}
	if u.Activity.IsZero() {
		t.Error("no activity time")
	}
	// Appended lines, one of them for m2 again, and a line still being written.
	lines = append(lines, assistant("m2", 2, 300, 1000, 80), assistant("m3", 1, 0, 1300, 5))
	os.WriteFile(file, []byte(strings.Join(lines, "\n")+"\n"+`{"type":"assistant","mess`), 0o644)
	u, _ = e.Usage(context.Background(), sid, state)
	if u.Output != 285 || u.Fresh() != 1592 || u.CacheRead != 2300 || u.Context != 1301 {
		t.Errorf("incremental: %+v", u)
	}
	// Rewritten from scratch (smaller): read again from the start.
	write(assistant("x", 1, 2, 3, 4))
	if u, _ = e.Usage(context.Background(), sid, state); u.Fresh() != 7 || u.CacheRead != 3 {
		t.Errorf("after a rewrite: %+v", u)
	}
	// Same size, other bytes before the offset: the checksum catches it.
	write(assistant("y", 1, 2, 3, 5))
	if u, _ = e.Usage(context.Background(), sid, state); u.Fresh() != 8 {
		t.Errorf("after a same-size rewrite: %+v", u)
	}
	// A transcript without usage (a gateway that reports none).
	write(`{"type":"assistant","message":{"id":"z","content":[]}}`)
	if u, err = e.Usage(context.Background(), sid, state); err != nil || u.Known {
		t.Errorf("no usage: %+v %v", u, err)
	}
	// No transcript at all.
	if _, err := e.Usage(context.Background(), "ffffffff-0000-4000-8000-000000000000", state); err == nil {
		t.Error("a missing transcript reads")
	}
}

// A line longer than the read buffer (a large tool input) is read whole.
func TestUsageReadsLongLines(t *testing.T) {
	h := testutil.NewHome(t)
	e := newEngine(h)
	sid := "0f0f0f0f-1111-4222-8333-444455551111"
	dir := filepath.Join(h.Claude, "projects", "-repo")
	os.MkdirAll(dir, 0o755)
	big := `{"type":"assistant","message":{"id":"big","content":[{"type":"tool_use","input":{"content":"` + strings.Repeat("x", 3<<20) +
		`"}}],"usage":{"input_tokens":1,"cache_creation_input_tokens":2,"cache_read_input_tokens":3,"output_tokens":40000}}}`
	os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(assistant("a", 1, 0, 0, 1)+"\n"+big+"\n"+assistant("b", 1, 0, 0, 1)+"\n"), 0o644)
	u, err := e.Usage(context.Background(), sid, filepath.Join(h.Csquad, "cache", "T3.read.json"))
	if err != nil || u.Output != 40002 || u.Fresh() != 40007 {
		t.Errorf("%+v %v", u, err)
	}
}

// Many messages: the ones that leave the open window are folded into the
// total, which stays exact.
func TestUsageFoldsOldMessages(t *testing.T) {
	h := testutil.NewHome(t)
	e := newEngine(h)
	sid := "0f0f0f0f-1111-4222-8333-444455550000"
	dir := filepath.Join(h.Claude, "projects", "-repo")
	os.MkdirAll(dir, 0o755)
	var lines []string
	for i := 0; i < 500; i++ {
		lines = append(lines, assistant(fmt.Sprint("m", i), 1, 10, 100, 1), assistant(fmt.Sprint("m", i), 1, 10, 100, 2))
	}
	os.WriteFile(filepath.Join(dir, sid+".jsonl"), []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	u, err := e.Usage(context.Background(), sid, filepath.Join(h.Csquad, "cache", "T2.read.json"))
	if err != nil || u.Fresh() != 500*13 || u.CacheRead != 500*100 {
		t.Errorf("%+v %v", u, err)
	}
}
