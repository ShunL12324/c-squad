package claude

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const transcript = `{"type":"user","message":{"content":"Fix the bug"}}
{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"}]}}
{"type":"user","message":{"content":[{"type":"tool_result","content":"file"}]}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Read"},{"type":"tool_use","name":"Bash"}]}}
{"type":"user","isMeta":true,"message":{"content":"<system-reminder>meta</system-reminder>"}}
{"type":"user","message":{"content":"<task-notification>done</task-notification>"}}
not json
{"type":"attachment"}
{"type":"assistant","isSidechain":true,"message":{"content":[{"type":"text","text":"subagent"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Found it."}]}}
{"type":"user","message":{"content":"please go on<system-reminder>x</system-reminder>"}}
{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit"}]}}
{"type":"assistant","message":{"content":[{"type":"text","text":"Fixed and committed."}]}}
`

func TestParseTranscript(t *testing.T) {
	entries, err := ParseTranscript(strings.NewReader(transcript))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range entries {
		if e.Role == RoleTools {
			var parts []string
			for _, c := range e.Tools {
				parts = append(parts, c.Name+"×"+string(rune('0'+c.Count)))
			}
			got = append(got, "tools:"+strings.Join(parts, ","))
		} else {
			got = append(got, e.Role+":"+e.Text)
		}
	}
	want := []string{"user:Fix the bug", "tools:Read×2,Bash×1", "assistant:Found it.", "user:please go on", "tools:Edit×1", "assistant:Fixed and committed."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("entries:\n%v\nwant\n%v", got, want)
	}
	if last := Last(entries, 2); len(last) != 3 || last[0].Text != "please go on" {
		t.Fatalf("Last(2) = %+v", last)
	}
	if last := Last(entries, 10); len(last) != len(entries) {
		t.Fatalf("Last(10) dropped entries: %+v", last)
	}
	if LastAssistant(entries) != "Fixed and committed." {
		t.Fatal("wrong last assistant text")
	}
}

func TestReadTranscriptFindsSessionInAnyProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	dir := filepath.Join(home, "projects", "-some-worktree")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "abcd1234-0000.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	entries, err := ReadTranscript("abcd1234")
	if err != nil || LastAssistant(entries) != "Fixed and committed." {
		t.Fatalf("ReadTranscript: %v %v", entries, err)
	}
	if _, err := ReadTranscript("ffff0000"); err == nil {
		t.Fatal("found a missing transcript")
	}
}

func TestEnsureTrusted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", home)
	state := filepath.Join(home, ".claude.json")
	original := `{"numStartups": 12345678901234567890, "projects": {"/trusted": {"hasTrustDialogAccepted": true, "x": 1}}}`
	if err := os.WriteFile(state, []byte(original), 0o640); err != nil {
		t.Fatal(err)
	}
	// A child of a trusted directory needs no write.
	if err := EnsureTrusted("/trusted/sub"); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(state); string(data) != original {
		t.Fatal("rewrote the state for an already trusted directory")
	}
	if err := EnsureTrusted("/new/project"); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(state)
	if !strings.Contains(string(data), "12345678901234567890") {
		t.Fatalf("large number not preserved: %s", data)
	}
	var parsed struct {
		Projects map[string]map[string]any `json:"projects"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Projects["/new/project"]["hasTrustDialogAccepted"] != true || parsed.Projects["/trusted"]["x"] != float64(1) {
		t.Fatalf("projects = %v", parsed.Projects)
	}
	if info, _ := os.Stat(state); info.Mode().Perm() != 0o640 {
		t.Fatalf("mode changed to %v", info.Mode().Perm())
	}
}

func TestLaunchPassesOptionsAndParsesID(t *testing.T) {
	dir := t.TempDir()
	argsFile := filepath.Join(dir, "args")
	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > " + argsFile + "\npwd >> " + argsFile + "\necho 'backgrounded · 2c544b21 · name'\n"
	if err := os.WriteFile(fake, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CSQUAD_CLAUDE", fake)
	id, err := Launch(context.Background(), LaunchOptions{Dir: dir, Name: "T1 · x", Prompt: "-starts with dash", SystemPrompt: "SYS", Agent: "reviewer", PermissionMode: "auto"})
	if err != nil || id != "2c544b21" {
		t.Fatalf("Launch = %q, %v", id, err)
	}
	args, _ := os.ReadFile(argsFile)
	want := "--bg\n--name\nT1 · x\n--agent\nreviewer\n--permission-mode\nauto\n--append-system-prompt\nSYS\n--\n-starts with dash\n"
	if !strings.HasPrefix(string(args), want) {
		t.Fatalf("args:\n%s\nwant prefix\n%s", args, want)
	}
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho 'Workspace not trusted.'\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := Launch(context.Background(), LaunchOptions{Dir: dir, Prompt: "x"}); err == nil || !strings.Contains(err.Error(), "Workspace not trusted") {
		t.Fatalf("launch failure lost the CLI output: %v", err)
	}
}
