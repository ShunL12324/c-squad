package claude

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/ShunL12324/c-squad/internal/paths"
)

// Roles of transcript entries.
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	// RoleTools is a folded run of tool calls between two messages.
	RoleTools = "tools"
)

// Entry is one conversation message, or a folded run of tool calls.
type Entry struct {
	Role string
	Text string
	// Tools counts tool calls by name, in first-use order, for RoleTools.
	Tools []ToolCount
}

// ToolCount is how often one tool ran in a folded run.
type ToolCount struct {
	Name  string
	Count int
}

// ErrNoTranscript reports a session whose transcript file cannot be found.
var ErrNoTranscript = errors.New("no transcript found")

// TranscriptPath finds the transcript of a session by its short or full ID.
// Background sessions move into worktrees, so the project directory is not
// derived from the task directory; every project is searched instead. When
// several files match, the most recently modified wins.
func TranscriptPath(id string) (string, error) {
	home, err := paths.ClaudeHome()
	if err != nil {
		return "", err
	}
	matches, err := filepath.Glob(filepath.Join(home, "projects", "*", id+"*.jsonl"))
	if err != nil {
		return "", err
	}
	if len(matches) == 0 {
		return "", fmt.Errorf("session %s: %w", id, ErrNoTranscript)
	}
	best, bestTime := "", int64(0)
	for _, m := range matches {
		if info, err := os.Stat(m); err == nil && info.ModTime().UnixNano() >= bestTime {
			best, bestTime = m, info.ModTime().UnixNano()
		}
	}
	return best, nil
}

// ReadTranscript returns the conversation of a session with noise removed.
func ReadTranscript(id string) ([]Entry, error) {
	path, err := TranscriptPath(id)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return ParseTranscript(f)
}

type rawLine struct {
	Type        string `json:"type"`
	Cwd         string `json:"cwd"`
	GitBranch   string `json:"gitBranch"`
	IsMeta      bool   `json:"isMeta"`
	IsSidechain bool   `json:"isSidechain"`
	Message     struct {
		Content json.RawMessage `json:"content"`
	} `json:"message"`
}

type rawBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text"`
	Name  string `json:"name"`
	Input struct {
		Command string `json:"command"`
	} `json:"input"`
}

var (
	reminder = regexp.MustCompile(`(?s)<system-reminder>.*?</system-reminder>`)
	// Harness wrappers such as <command-name>, <local-command-stdout> or
	// <task-notification> open a user entry that the user did not type.
	wrapper = regexp.MustCompile(`^<[a-z][a-z0-9-]*>`)
)

// ParseTranscript reads Claude Code JSONL. It keeps user and assistant text,
// folds tool calls into counts, and drops tool results, thinking, metadata,
// sidechains, system reminders and harness wrappers. Unknown or malformed lines
// are skipped because the format is internal to Claude Code.
func ParseTranscript(r io.Reader) ([]Entry, error) {
	var out []Entry
	var tools []ToolCount
	flush := func() {
		if len(tools) > 0 {
			out = append(out, Entry{Role: RoleTools, Tools: tools})
			tools = nil
		}
	}
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			var raw rawLine
			if json.Unmarshal(line, &raw) == nil && !raw.IsMeta && !raw.IsSidechain && (raw.Type == RoleUser || raw.Type == RoleAssistant) {
				for _, b := range blocks(raw.Message.Content) {
					switch {
					case b.Type == "tool_use" && raw.Type == RoleAssistant:
						i := slices.IndexFunc(tools, func(t ToolCount) bool { return t.Name == b.Name })
						if i < 0 {
							tools = append(tools, ToolCount{Name: b.Name})
							i = len(tools) - 1
						}
						tools[i].Count++
					case b.Type == "text":
						text := strings.TrimSpace(b.Text)
						if raw.Type == RoleUser {
							text = strings.TrimSpace(reminder.ReplaceAllString(text, ""))
							if wrapper.MatchString(text) {
								text = ""
							}
						}
						if text != "" {
							flush()
							out = append(out, Entry{Role: raw.Type, Text: text})
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
	}
	flush()
	return out, nil
}

// Location is a directory a session worked in and the branch checked out there.
type Location struct {
	Cwd    string
	Branch string
}

// Trail is where a session worked and which branches it created.
type Trail struct {
	// Locations are the distinct directories and checked-out branches of the
	// main conversation, in first-seen order. A session that entered a
	// worktree shows it here even after it left again.
	Locations []Location
	// Created are branch names the session's shell commands created. A branch
	// created and left within one command never appears in Locations.
	Created []string
}

// createdBranch matches git commands that create a branch: checkout -b/-B,
// switch -c/-C, worktree add -b/-B, and `git branch NAME`.
var createdBranch = regexp.MustCompile(`\bgit\b[^;&|\n]*?\s(?:checkout\s+-[bB]|switch\s+-[cC]|worktree\s+add\s+(?:\S+\s+)*?-[bB]|branch)\s+([A-Za-z0-9._][A-Za-z0-9._/-]*)`)

// ReadTrail returns the trail of a session.
func ReadTrail(id string) (Trail, error) {
	path, err := TranscriptPath(id)
	if err != nil {
		return Trail{}, err
	}
	f, err := os.Open(path)
	if err != nil {
		return Trail{}, err
	}
	defer func() { _ = f.Close() }()
	return ParseTrail(f)
}

// ParseTrail extracts the trail from transcript JSONL.
func ParseTrail(r io.Reader) (Trail, error) {
	var out Trail
	reader := bufio.NewReader(r)
	for {
		line, err := reader.ReadBytes('\n')
		var raw rawLine
		if len(line) > 0 && json.Unmarshal(line, &raw) == nil && !raw.IsSidechain {
			if loc := (Location{Cwd: raw.Cwd, Branch: raw.GitBranch}); raw.Cwd != "" && !slices.Contains(out.Locations, loc) {
				out.Locations = append(out.Locations, loc)
			}
			if raw.Type == RoleAssistant {
				for _, b := range blocks(raw.Message.Content) {
					if b.Type != "tool_use" || b.Input.Command == "" {
						continue
					}
					for _, m := range createdBranch.FindAllStringSubmatch(b.Input.Command, -1) {
						if !slices.Contains(out.Created, m[1]) {
							out.Created = append(out.Created, m[1])
						}
					}
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return Trail{}, err
		}
	}
}

// blocks normalises message content, which is either a string or a list of
// typed blocks.
func blocks(content json.RawMessage) []rawBlock {
	var s string
	if json.Unmarshal(content, &s) == nil {
		return []rawBlock{{Type: "text", Text: s}}
	}
	var list []rawBlock
	_ = json.Unmarshal(content, &list)
	return list
}

// Last returns the tail of entries holding the last n messages, together with
// the tool runs between them. Tool runs before the first kept message are
// dropped, so the tail always starts with a message.
func Last(entries []Entry, n int) []Entry {
	if n <= 0 {
		return nil
	}
	seen := 0
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role != RoleTools {
			seen++
			if seen == n {
				return entries[i:]
			}
		}
	}
	for i, e := range entries {
		if e.Role != RoleTools {
			return entries[i:]
		}
	}
	return nil
}

// LastAssistant returns the most recent assistant text, or "".
func LastAssistant(entries []Entry) string {
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Role == RoleAssistant {
			return entries[i].Text
		}
	}
	return ""
}
