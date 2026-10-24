// Package check runs one check of a task: a fresh read-only reviewer (Codex
// or Claude) that answers with a structured verdict, or a shell command run
// in the task's worktree.
package check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/engine"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// Kinds of check.
const (
	Cmd    = "cmd"
	Claude = "claude"
	Codex  = "codex"
)

// Spec is a parsed --check value: codex[:model], claude[:model] or
// cmd:<command>.
type Spec struct {
	Kind    string
	Model   string // reviewers
	Command string // cmd
}

// Reviewer reports whether the check is an agent's review (not a command).
func (s Spec) Reviewer() bool { return s.Kind != Cmd }

func (s Spec) String() string {
	switch {
	case s.Kind == Cmd:
		return "cmd:" + s.Command
	case s.Model != "":
		return s.Kind + ":" + s.Model
	}
	return s.Kind
}

// ParseSpec reads a --check value.
func ParseSpec(v string) (Spec, error) {
	kind, rest, _ := strings.Cut(strings.TrimSpace(v), ":")
	switch kind {
	case Cmd:
		if strings.TrimSpace(rest) == "" {
			return Spec{}, fmt.Errorf(`--check cmd: needs a command, e.g. --check 'cmd:go test ./...'`)
		}
		return Spec{Kind: Cmd, Command: rest}, nil
	case Claude, Codex:
		return Spec{Kind: kind, Model: strings.TrimSpace(rest)}, nil
	}
	return Spec{}, fmt.Errorf("--check must be codex[:model], claude[:model] or cmd:<command>, got %q", v)
}

// Finding is one problem a check found.
type Finding struct {
	Severity string `json:"severity"` // blocking | minor
	File     string `json:"file"`
	Summary  string `json:"summary"`
}

// Verdict is a check's result. Error set means there was no verdict.
type Verdict struct {
	Verdict  string    `json:"verdict,omitempty"` // pass | fail
	Summary  string    `json:"summary,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
	Error    string    `json:"error,omitempty"`
	Round    int       `json:"round,omitempty"`
	For      int64     `json:"for,omitempty"` // the task version (updated_at) it was run for
	At       int64     `json:"at,omitempty"`  // unix milliseconds
	// Blocking counts the blocking findings of every round so far (the
	// convergence guard reads it); kept in the task's last_check.
	Blocking []int `json:"blocking,omitempty"`
}

// Pass reports a passing verdict.
func (v Verdict) Pass() bool { return v.Error == "" && v.Verdict == "pass" }

// Count is the number of blocking findings (a finding without severity
// counts as blocking).
func (v Verdict) Count() int {
	n := 0
	for _, f := range v.Findings {
		if f.Severity != "minor" {
			n++
		}
	}
	return n
}

// Schema is the verdict's JSON schema, given to reviewers.
var Schema = []byte(`{"type":"object","additionalProperties":false,"required":["verdict","summary","findings"],"properties":{` +
	`"verdict":{"type":"string","enum":["pass","fail"]},` +
	`"summary":{"type":"string"},` +
	`"findings":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["severity","file","summary"],"properties":{` +
	`"severity":{"type":"string","enum":["blocking","minor"]},"file":{"type":"string"},"summary":{"type":"string"}}}}}}`)

var fence = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// Parse finds a verdict in a reviewer's answer: the whole answer as JSON, a
// code fence, or the last JSON object after prose.
func Parse(text string) (Verdict, bool) {
	text = strings.TrimSpace(text)
	if v, ok := decode([]byte(text)); ok {
		return v, true
	}
	if m := fence.FindAllStringSubmatch(text, -1); m != nil {
		for i := len(m) - 1; i >= 0; i-- {
			if v, ok := decode([]byte(m[i][1])); ok {
				return v, true
			}
		}
	}
	var found Verdict
	ok := false
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		var raw json.RawMessage
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&raw) != nil {
			continue
		}
		if v, good := decode(raw); good {
			found, ok = v, true
			i += len(raw) - 1
		}
	}
	return found, ok
}

func decode(b []byte) (Verdict, bool) {
	var v Verdict
	if json.Unmarshal(b, &v) != nil || (v.Verdict != "pass" && v.Verdict != "fail") {
		return Verdict{}, false
	}
	v.Error, v.Round, v.For, v.At, v.Blocking = "", 0, 0, 0, nil
	return v, true
}

// Request is what one check looks at.
type Request struct {
	ID       int
	Title    string
	Prompt   string // the task's prompt
	Final    string // the worker's final message
	Dir      string // the worktree (or the checkout of an in-place task)
	Branch   string // "" for an in-place task
	Target   string // the branch to compare against ("" when unknown)
	Round    int
	Max      int
	Previous *Verdict // the previous round's verdict, if it failed
	Env      proc.Env
	Log      io.Writer
	Timeout  time.Duration // default 30 min
	Poll     time.Duration // agent reviewers; default 3 s
	// Engine returns an engine by name (the Codex reviewer).
	Engine func(name string) (engine.Engine, error)
}

func (r Request) logf(format string, args ...any) {
	if r.Log != nil {
		fmt.Fprintf(r.Log, format+"\n", args...)
	}
}

// Run runs one check and returns its verdict; a check that cannot decide
// returns a verdict with Error set.
func Run(ctx context.Context, s Spec, r Request) Verdict {
	if r.Timeout == 0 {
		r.Timeout = 30 * time.Minute
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	var v Verdict
	switch s.Kind {
	case Cmd:
		v = runCmd(ctx, s.Command, r)
	case Claude:
		v = runClaude(ctx, s.Model, r)
	case Codex:
		v = runAgent(ctx, Codex, s.Model, r)
	default:
		v = Verdict{Error: "unknown check " + s.String()}
	}
	if v.Error == "" && v.Verdict == "fail" && len(v.Findings) == 0 {
		v.Findings = []Finding{{Severity: "blocking", Summary: v.Summary}}
	}
	if ctx.Err() == context.DeadlineExceeded && v.Error == "" && !v.Pass() && v.Verdict == "" {
		v.Error = fmt.Sprintf("timed out after %s", r.Timeout)
	}
	return v
}

// tail is the end of a command's output: at most lines lines and bytes bytes.
func tail(out string, lines, bytes int) string {
	out = strings.TrimRight(out, "\n")
	parts := strings.Split(out, "\n")
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	out = strings.Join(parts, "\n")
	if len(out) > bytes {
		out = "…" + out[len(out)-bytes:]
	}
	return out
}

// runCmd runs the command with the shell in the task's directory: exit 0
// passes, otherwise the end of its output is the finding.
func runCmd(ctx context.Context, command string, r Request) Verdict {
	c := proc.Shell(command, r.Env)
	cmd := c.Command(ctx)
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	cmd.WaitDelay = 5 * time.Second
	proc.KillGroup(cmd)
	var out bytes.Buffer
	var w io.Writer = &out
	if r.Log != nil {
		w = io.MultiWriter(&out, r.Log)
	}
	cmd.Stdout, cmd.Stderr = w, w
	r.logf("$ %s   (in %s)", command, r.Dir)
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Verdict{Error: fmt.Sprintf("`%s` did not finish within %s", command, r.Timeout)}
	}
	if err == nil {
		return Verdict{Verdict: "pass", Summary: fmt.Sprintf("`%s` exited 0", command)}
	}
	var ee *exec.ExitError
	if !errors.As(err, &ee) {
		return Verdict{Error: fmt.Sprintf("cannot run `%s`: %v", command, err)}
	}
	text := tail(out.String(), 60, 6000)
	if text == "" {
		text = "(no output)"
	}
	return Verdict{Verdict: "fail", Summary: fmt.Sprintf("`%s` exited %d", command, ee.ExitCode()),
		Findings: []Finding{{Severity: "blocking", Summary: text}}}
}

// Prompt is a reviewer's prompt.
func Prompt(r Request) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You review the work of the background coding task T%d (%q), check round %d of %d. You are read-only: do not edit files, commit, or run anything that changes the repository or its worktrees.\n\n", r.ID, r.Title, r.Round, r.Max)
	fmt.Fprintf(&b, "The task's prompt:\n<<<\n%s\n>>>\n\n", strings.TrimSpace(r.Prompt))
	final := strings.TrimSpace(r.Final)
	if final == "" {
		final = "(none)"
	}
	fmt.Fprintf(&b, "The worker's final message:\n<<<\n%s\n>>>\n\n", final)
	if r.Branch != "" {
		target := r.Target
		if target == "" {
			target = "the branch it started from"
		}
		fmt.Fprintf(&b, "Review branch %s, checked out in your working directory %s, against %s:", r.Branch, r.Dir, target)
		if r.Target != "" {
			fmt.Fprintf(&b, " `git diff %s...HEAD`, `git log %s..HEAD`.", r.Target, r.Target)
		}
	} else {
		fmt.Fprintf(&b, "Review the uncommitted changes in your working directory %s: `git status`, `git diff`, `git diff --cached`.", r.Dir)
	}
	b.WriteString(" Run git in the working directory as it is, without -C. Read the code around the changes as needed.\n\n")
	if p := r.Previous; p != nil && len(p.Findings) > 0 {
		b.WriteString("The previous round found these; the worker was asked to fix them:\n")
		for _, f := range p.Findings {
			b.WriteString("- " + FormatFinding(f) + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Decide whether the work does what the task asks, correctly, without breaking what was there. Report real problems only; mark a finding blocking only when it must be fixed before the work is merged.\n\n")
	b.WriteString(`Answer with only this JSON object: {"verdict": "pass" or "fail", "summary": "one or two sentences", "findings": [{"severity": "blocking" or "minor", "file": "path or empty", "summary": "what is wrong and how to fix it"}]}. The verdict is "fail" exactly when there is a blocking finding.` + "\n")
	return b.String()
}

// FormatFinding is a finding on one line for the worker.
func FormatFinding(f Finding) string {
	s := f.Summary
	if f.File != "" {
		s = f.File + ": " + s
	}
	if f.Severity != "" {
		s = "[" + f.Severity + "] " + s
	}
	return s
}

// ClaudeTools are the tools the Claude reviewer may use without asking.
var ClaudeTools = []string{"Read", "Grep", "Glob",
	"Bash(git diff:*)", "Bash(git log:*)", "Bash(git show:*)", "Bash(git status:*)", "Bash(git branch:*)",
	"Bash(git rev-parse:*)", "Bash(git merge-base:*)", "Bash(git ls-files:*)", "Bash(git blame:*)"}

// ClaudeArgs is the Claude reviewer's command line (the prompt goes on stdin).
func ClaudeArgs(model string) []string {
	args := []string{"-p", "--output-format", "json", "--json-schema", string(Schema), "--permission-mode", "dontAsk",
		"--no-session-persistence", "--tools", "Read,Grep,Glob,Bash", "--allowedTools", strings.Join(ClaudeTools, ",")}
	if model != "" {
		args = append(args, "--model", model)
	}
	return args
}

// runClaude runs a fresh `claude -p` reviewer with the verdict schema and
// reads its structured_output.
func runClaude(ctx context.Context, model string, r Request) Verdict {
	c, err := proc.Resolve("claude", ClaudeArgs(model), r.Env)
	if err != nil {
		return Verdict{Error: err.Error()}
	}
	cmd := c.Command(ctx)
	cmd.Dir = r.Dir
	cmd.Env = r.Env
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = strings.NewReader(Prompt(r))
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	r.logf("$ claude %s   (in %s)", strings.Join(redactSchema(ClaudeArgs(model)), " "), r.Dir)
	err = cmd.Run()
	r.logf("%s%s", out.String(), errb.String())
	var res struct {
		Result     string          `json:"result"`
		IsError    bool            `json:"is_error"`
		Structured json.RawMessage `json:"structured_output"`
	}
	if json.Unmarshal(out.Bytes(), &res) != nil {
		msg := strings.TrimSpace(errb.String() + out.String())
		if err != nil && msg == "" {
			msg = err.Error()
		}
		return Verdict{Error: "the Claude reviewer printed no result: " + tail(msg, 5, 500)}
	}
	if len(res.Structured) > 0 {
		if v, ok := decode(res.Structured); ok {
			return v
		}
	}
	if v, ok := Parse(res.Result); ok {
		return v
	}
	return Verdict{Error: "the Claude reviewer gave no verdict: " + tail(res.Result, 5, 500)}
}

func redactSchema(args []string) []string {
	out := append([]string(nil), args...)
	for i := range out {
		if i > 0 && out[i-1] == "--json-schema" {
			out[i] = "<verdict schema>"
		}
	}
	return out
}

// CodexArgs are the reviewer thread's options: read-only, never asking.
var CodexArgs = []string{"-s", "read-only", "-a", "never"}

// runAgent runs a reviewer through an engine (Codex): a fresh read-only
// session named "T12 · check N" with the verdict schema, polled until its
// turn ends, then removed.
func runAgent(ctx context.Context, name, model string, r Request) Verdict {
	if r.Engine == nil {
		return Verdict{Error: "no engine for the " + name + " reviewer"}
	}
	eng, err := r.Engine(name)
	if err != nil {
		return Verdict{Error: err.Error()}
	}
	args, err := eng.CheckArgs(CodexArgs, r.Dir)
	if err != nil {
		return Verdict{Error: err.Error()}
	}
	started, err := eng.Start(ctx, engine.StartSpec{
		ID: r.ID, Title: fmt.Sprintf("check %d", r.Round), Cwd: r.Dir, Model: model, Prompt: Prompt(r), Args: args, Schema: Schema,
	})
	if err != nil {
		return Verdict{Error: "starting the reviewer: " + err.Error()}
	}
	sid := started.SessionID
	r.logf("reviewer session %s", sid)
	defer func() {
		// The reviewer is removed even when the check was cut short.
		rctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if how, err := eng.Remove(rctx, sid, nil); err != nil {
			r.logf("removing the reviewer: %v", err)
		} else {
			r.logf("%s", how)
		}
	}()
	poll := r.Poll
	if poll == 0 {
		poll = 3 * time.Second
	}
	start := time.Now()
	for {
		states, err := eng.States(ctx, []string{sid})
		if err == nil {
			l := states[sid]
			switch l.State {
			case engine.Failed, engine.Gone:
				return Verdict{Error: "the reviewer " + stateWord(l.State)}
			case engine.Idle, engine.Done, engine.Stopped, engine.NeedsInput:
				msgs, _ := eng.Last(ctx, sid, 0)
				final := engine.LastAssistant(msgs)
				if final != "" || time.Since(start) > 2*time.Minute {
					r.logf("reviewer answered:\n%s", final)
					if v, ok := Parse(final); ok {
						return v
					}
					return Verdict{Error: "the reviewer gave no verdict: " + tail(final, 5, 500)}
				}
			}
		} else {
			r.logf("observing the reviewer: %v", err)
		}
		select {
		case <-ctx.Done():
			return Verdict{Error: fmt.Sprintf("the reviewer did not answer within %s", r.Timeout)}
		case <-time.After(poll):
		}
	}
}

func stateWord(s string) string {
	if s == engine.Gone {
		return "session is gone"
	}
	return "failed"
}
