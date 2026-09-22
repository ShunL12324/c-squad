package squad

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"
)

// Test 18: the member table lost its role column, so the line each member is
// listed with is now its responsibilities, reduced to a single cell.
func TestMemberTableSummarisesInstructions(t *testing.T) {
	long := "Own the payments API\nand nothing else"
	members := map[string]*Member{
		"a":       {ID: "a", State: MemberStateIdle, Engine: "codex", Instructions: "Review   the candidate\nsecond paragraph"},
		"b":       {ID: "b", State: MemberStateIdle, Engine: "codex", Instructions: strings.Repeat("responsibility ", 12)},
		"unnamed": {ID: "unnamed", State: MemberStateIdle, Engine: "codex"},
		"master":  {ID: "master", State: MemberStateIdle, Engine: "claude", Instructions: long},
	}
	var out bytes.Buffer
	must(t, writeTable(&out, members))
	text := out.String()
	header, rest, _ := strings.Cut(text, "\n")
	if !strings.Contains(header, "INSTRUCTIONS") || strings.Contains(header, "ROLE") {
		t.Fatalf("member table header: %q", header)
	}
	// Only the first line reaches the table, with its internal whitespace
	// collapsed, and a long one is cut rather than wrapping the row.
	if !strings.Contains(rest, "Review the candidate") || strings.Contains(rest, "second paragraph") {
		t.Fatalf("instructions summary: %q", rest)
	}
	for _, line := range strings.Split(strings.TrimSpace(rest), "\n") {
		if len([]rune(line)) > 200 {
			t.Fatalf("row too wide: %q", line)
		}
		if strings.HasPrefix(line, "b ") && !strings.Contains(line, "…") {
			t.Fatalf("long instructions were not truncated: %q", line)
		}
	}
	if summary := instructionsSummary(""); summary != "" {
		t.Fatalf("a member without instructions must leave the cell blank: %q", summary)
	}
	if summary := instructionsSummary(long); summary != "Own the payments API" {
		t.Fatalf("first line only: %q", summary)
	}
}

// Test 19: member inspect is where the full responsibilities are read, and they
// appear exactly once: the same prose printed twice leaves a reader unsure which
// copy is current.
func TestMemberInspectSurfacesInstructionsOnce(t *testing.T) {
	st := testStore(t)
	instructions := "Own the refund endpoint and verify idempotency"
	must(t, st.update(func(s *State) error {
		s.Members["a"].Instructions = instructions
		return nil
	}))
	f, err := os.CreateTemp(t.TempDir(), "inspect")
	must(t, err)
	prior := os.Stdout
	os.Stdout = f
	err = memberCommand(st, "master", []string{"inspect", "a"}, options{})
	os.Stdout = prior
	must(t, err)
	_, err = f.Seek(0, 0)
	must(t, err)
	raw, err := io.ReadAll(f)
	must(t, err)
	must(t, f.Close())
	var record map[string]any
	must(t, json.Unmarshal(raw, &record))
	if record["instructions"] != instructions {
		t.Fatalf("inspect does not surface instructions: %s", raw)
	}
	if strings.Count(string(raw), instructions) != 1 {
		t.Fatalf("instructions are printed more than once: %s", raw)
	}
	if member, ok := record["member"].(map[string]any); !ok || member["instructions"] != nil || member["role"] != nil {
		t.Fatalf("the member record still carries identity fields: %s", raw)
	}
}

// Test 21: the removed launch flags are refused where a member is created, so a
// script written for the old command line stops instead of silently launching
// something its author did not ask for.
func TestMemberAddRejectsRemovedLaunchFlags(t *testing.T) {
	st := testStore(t)
	for flag, value := range map[string]string{"engine": "claude", "model": "opus", "env": "CODEX_HOME=/x", "role": "reviewer", "template": "developer"} {
		err := memberCommand(st, "master", []string{"add", "new"}, options{flag: value, "instructions": "work"})
		if err == nil || !strings.Contains(err.Error(), "--"+flag+" was removed") {
			t.Fatalf("member add --%s: %v", flag, err)
		}
	}
}
