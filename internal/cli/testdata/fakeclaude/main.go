// Command fakeclaude imitates the parts of the claude CLI that csquad drives,
// keeping sessions as JSON files in $FAKE_CLAUDE_STATE:
//
//	claude --bg --name N [flags] -- PROMPT   start a session
//	claude agents --json --all               list sessions
//	claude rm ID [--discard-unpushed TOKEN]  remove a session
//
// Prompts steer it: LAUNCH_FAIL makes --bg fail, SLOW_LAUNCH delays it.
// A file ID.unpushed makes rm refuse without the discard token, and a file
// AGENTS_FAIL makes the listing fail.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type session struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Name      string `json:"name"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	State     string `json:"state"`
}

func main() {
	state := os.Getenv("FAKE_CLAUDE_STATE")
	args := os.Args[1:]
	switch {
	case len(args) > 0 && args[0] == "--bg":
		launch(state, args)
	case len(args) > 0 && args[0] == "agents":
		list(state)
	case len(args) > 1 && args[0] == "rm":
		remove(state, args[1], slices.Contains(args, "--discard-unpushed"))
	default:
		fail("unsupported arguments: %q", args)
	}
}

func launch(state string, args []string) {
	name, prompt := "", ""
	for i, a := range args {
		if a == "--name" && i+1 < len(args) {
			name = args[i+1]
		}
		if a == "--" && i+1 < len(args) {
			prompt = args[i+1]
		}
	}
	if strings.Contains(prompt, "LAUNCH_FAIL") {
		fail("Workspace not trusted. Run `claude` in the directory once.")
	}
	if strings.Contains(prompt, "SLOW_LAUNCH") {
		time.Sleep(2 * time.Second)
	}
	cwd, _ := os.Getwd()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	s := session{ID: id, SessionID: id + "-0000-4000-8000-000000000000", Name: name, Cwd: cwd, Kind: "background", Status: "busy", State: "working"}
	// Record the peak number of working sessions, including this one.
	working := 1
	for _, other := range sessions(state) {
		if other.State == "working" || other.State == "blocked" {
			working++
		}
	}
	peak := filepath.Join(state, "peak")
	if old, err := os.ReadFile(peak); err != nil || atoi(string(old)) < working {
		must(os.WriteFile(peak, []byte(fmt.Sprint(working)), 0o600))
	}
	data, _ := json.Marshal(s)
	must(os.WriteFile(filepath.Join(state, id+".json"), data, 0o600))
	must(os.WriteFile(filepath.Join(state, id+".args"), []byte(strings.Join(args, "\n")), 0o600))
	dir := filepath.Join(os.Getenv("CLAUDE_CONFIG_DIR"), "projects", "-fake")
	must(os.MkdirAll(dir, 0o700))
	line := func(v any) string { b, _ := json.Marshal(v); return string(b) + "\n" }
	transcript := line(map[string]any{"type": "user", "cwd": cwd, "gitBranch": "main", "message": map[string]any{"content": prompt}}) +
		line(map[string]any{"type": "assistant", "cwd": cwd, "gitBranch": "main", "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": "Working on " + name}}}})
	must(os.WriteFile(filepath.Join(dir, s.SessionID+".jsonl"), []byte(transcript), 0o600))
	fmt.Printf("backgrounded · %s · %s\n", id, name)
}

func list(state string) {
	if _, err := os.Stat(filepath.Join(state, "AGENTS_FAIL")); err == nil {
		fail("agents unavailable")
	}
	data, _ := json.Marshal(sessions(state))
	fmt.Println(string(data))
}

func remove(state, id string, discard bool) {
	path := filepath.Join(state, id+".json")
	if _, err := os.Stat(path); err != nil {
		fail("no session %s", id)
	}
	if _, err := os.Stat(filepath.Join(state, id+".unpushed")); err == nil && !discard {
		fail("  1 unpushed commit on worktree-x.\n  push them and run 'claude rm %s' again, or discard: claude rm %s --discard-unpushed tok-%s", id, id, id)
	}
	must(os.Remove(path))
	fmt.Println("removed", id)
}

func sessions(state string) []session {
	out := []session{}
	files, _ := filepath.Glob(filepath.Join(state, "*.json"))
	for _, f := range files {
		var s session
		if data, err := os.ReadFile(f); err == nil && json.Unmarshal(data, &s) == nil {
			out = append(out, s)
		}
	}
	return out
}

func atoi(s string) int {
	n := 0
	_, _ = fmt.Sscan(s, &n)
	return n
}

func must(err error) {
	if err != nil {
		fail("%v", err)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
