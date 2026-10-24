package codex

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/engine"
)

func tokenCountLine(in, cached, out, lastTotal int) string {
	return fmt.Sprintf(`{"timestamp":"t","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":%d,"cached_input_tokens":%d,"cache_write_input_tokens":0,"output_tokens":%d,"total_tokens":%d},"last_token_usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1,"total_tokens":%d},"model_context_window":258400},"rate_limits":{}}}`,
		in, cached, out, in+out, lastTotal)
}

func TestUsageReadsTheNewestTokenCount(t *testing.T) {
	f := setup(t, true)
	res, err := f.eng.Start(ctx(t), engine.StartSpec{ID: 1, Title: "x", Cwd: "/", Prompt: "p"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.SessionID
	rollout := filepath.Join(f.srv.Home, "sessions", id+".jsonl")
	os.MkdirAll(filepath.Dir(rollout), 0o755)
	filler := `{"type":"response_item","payload":{"type":"message","content":"` + strings.Repeat("x", 70000) + `"}}`
	lines := []string{
		tokenCountLine(1000, 600, 10, 1010),
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		tokenCountLine(37605, 29696, 122, 19638),
		filler, // longer than one chunk read backwards
		`{"type":"event_msg","payload":{"type":"token_count","info":null}}`,
		filler,
	}
	os.WriteFile(rollout, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	state := filepath.Join(t.TempDir(), "T1.read.json")
	u, err := f.eng.Usage(ctx(t), id, state)
	if err != nil || !u.Known || u.Input != 37605-29696 || u.CacheRead != 29696 || u.Output != 122 || u.Fresh() != 37605-29696+122 ||
		u.Context != 19638 || u.ContextMax != 258400 || u.Activity.IsZero() {
		t.Fatalf("%+v %v", u, err)
	}
	// The path is kept: the next reading asks the daemon nothing.
	f.srv.Reset()
	if _, err := f.eng.Usage(ctx(t), id, state); err != nil || len(f.srv.Calls()) != 0 {
		t.Errorf("asked again: %v %v", f.srv.Methods(), err)
	}
	// A rollout without usage yet.
	os.WriteFile(rollout, []byte(filler+"\n"), 0o644)
	if u, err := f.eng.Usage(ctx(t), id, state); err != nil || u.Known {
		t.Errorf("no usage: %+v %v", u, err)
	}
}
