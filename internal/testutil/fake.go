// Package testutil gives tests a fake `claude` and temporary homes, so no
// test touches the real ~/.claude or ~/.csquad or starts the real claude.
//
// The fake claude is the test binary itself: a `claude` link to it in a
// temporary bin directory on PATH. Each test package's TestMain calls
// MaybeFakeClaude first.
package testutil

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Session is one entry of the fake `claude agents --json --all`.
type Session struct {
	ID         string `json:"id"`
	SessionID  string `json:"sessionId"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	State      string `json:"state,omitempty"`
	Status     string `json:"status,omitempty"`
	Tempo      string `json:"tempo,omitempty"`
	WaitingFor any    `json:"waitingFor,omitempty"`
	Cwd        string `json:"cwd"`
	StartedAt  int64  `json:"startedAt"`
}

// MaybeFakeClaude runs the fake claude and exits when the binary was started
// as `claude`; it returns otherwise.
func MaybeFakeClaude() {
	base := strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe")
	if base != "claude" {
		return
	}
	os.Exit(fakeClaude(os.Args[1:]))
}

func readState(file string) ([]Session, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var list []Session
	err = json.Unmarshal(b, &list)
	return list, err
}

func writeState(file string, list []Session) {
	b, _ := json.Marshal(list)
	tmp := file + "." + strconv.Itoa(os.Getpid())
	os.WriteFile(tmp, b, 0o644)
	os.Rename(tmp, file)
}

func record(state string, args []string) {
	b, _ := json.Marshal(args)
	f, err := os.OpenFile(state+".argv", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err == nil {
		f.Write(append(b, '\n'))
		f.Close()
	}
}

func lock(state string) func() {
	l := state + ".lock"
	for i := 0; i < 2000; i++ {
		if err := os.Mkdir(l, 0o700); err == nil {
			return func() { os.Remove(l) }
		}
		time.Sleep(5 * time.Millisecond)
	}
	return func() {}
}

func arg(args []string, flag string) string {
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func has(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func fakeClaude(args []string) int {
	state := os.Getenv("FAKE_STATE")
	if len(args) == 0 {
		return 1
	}
	switch {
	case args[0] == "--version":
		v := os.Getenv("FAKE_VERSION")
		if v == "" {
			v = "2.1.295"
		}
		fmt.Println(v + " (Claude Code)")
		return 0
	case args[0] == "agents":
		if ms, _ := strconv.Atoi(os.Getenv("FAKE_AGENTS_SLEEP")); ms > 0 {
			time.Sleep(time.Duration(ms) * time.Millisecond)
		}
		if os.Getenv("FAKE_AGENTS_FAIL") != "" {
			fmt.Fprintln(os.Stderr, "agents failed")
			return 1
		}
		b, err := os.ReadFile(state)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 1
		}
		if f, err := os.OpenFile(state+".calls", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			f.WriteString("x")
			f.Close()
		}
		os.Stdout.Write(b)
		return 0
	case args[0] == "rm" || args[0] == "stop":
		record(state, args)
		unlock := lock(state)
		defer unlock()
		list, _ := readState(state)
		if args[0] == "rm" && os.Getenv("FAKE_RM_REFUSE") != "" && !has(args, "--discard-unpushed") {
			fmt.Println("Session " + args[1] + "'s worktree has unpushed commits. To discard them, run: claude rm " + args[1] + " --discard-unpushed 0123abc@wt-1")
			return 1
		}
		var out []Session
		for _, s := range list {
			if args[0] == "rm" && (s.ID == args[1] || s.SessionID == args[1]) && s.Cwd != "/" && os.Getenv("FAKE_RM_NEEDS_WORKTREE_GONE") != "" {
				// Like claude rm on a worktree with commits it has not seen pushed.
				if _, err := os.Stat(s.Cwd); err == nil && strings.Contains(s.Cwd, "worktrees") {
					fmt.Println("refusing: the worktree " + s.Cwd + " still exists")
					return 1
				}
			}
			if s.ID == args[1] || s.SessionID == args[1] {
				if args[0] == "stop" {
					s.State, s.Status = "stopped", ""
					out = append(out, s)
				}
				continue
			}
			out = append(out, s)
		}
		writeState(state, out)
		return 0
	case has(args, "--bg") && has(args, "--resume"):
		record(state, args)
		fmt.Println("woke session " + arg(args, "--resume"))
		return 0
	case has(args, "--bg"):
		name := arg(args, "--name")
		if max, _ := strconv.Atoi(os.Getenv("FAKE_MAX_ARG")); max > 0 {
			for _, a := range args {
				if len(a) > max {
					fmt.Fprintln(os.Stderr, "Argument list too long")
					return 1
				}
			}
		}
		record(state, args)
		if strings.Contains(name, "FAILME") {
			fmt.Fprintln(os.Stderr, "workspace not trusted")
			return 1
		}
		var b [4]byte
		rand.Read(b[:])
		id := hex.EncodeToString(b[:])
		cwd, _ := os.Getwd()
		time.Sleep(20 * time.Millisecond)
		unlock := lock(state)
		list, _ := readState(state)
		list = append(list, Session{ID: id, SessionID: id + "-0000-4000-8000-000000000000", Kind: "background", Name: name, State: "working", Status: "busy", Cwd: cwd, StartedAt: time.Now().UnixMilli()})
		writeState(state, list)
		unlock()
		fmt.Printf("\x1b[32mbackgrounded\x1b[0m · \x1b[1m%s\x1b[0m · %s\n", id, name)
		return 0
	}
	fmt.Fprintln(os.Stderr, "fake claude: unknown command", args)
	return 1
}

// Home is a temporary environment: HOME, CSQUAD_HOME, CLAUDE_CONFIG_DIR, a
// bin directory with the fake claude, and the fake's state file.
type Home struct {
	Root      string
	Home      string // HOME
	Csquad    string // CSQUAD_HOME
	Claude    string // CLAUDE_CONFIG_DIR
	Bin       string
	State     string
	ExtraEnv  []string
	testingTB testing.TB
}

// NewHome makes the directories and links the fake claude.
func NewHome(t testing.TB) *Home {
	t.Helper()
	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h := &Home{Root: root, Home: filepath.Join(root, "home"), Csquad: filepath.Join(root, "home", ".csquad"),
		Claude: filepath.Join(root, "home", ".claude"), Bin: filepath.Join(root, "bin"), State: filepath.Join(root, "sessions.json"), testingTB: t}
	for _, d := range []string{h.Home, h.Bin} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	name := "claude"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.Symlink(exe, filepath.Join(h.Bin, name)); err != nil {
		t.Fatal(err)
	}
	h.SetSessions(nil)
	return h
}

var providerVar = regexp.MustCompile(`^(ANTHROPIC_|CLAUDE_CODE_|CLAUDECODE|CLAUDE_CONFIG_DIR=|CSQUAD_|HOME=|USERPROFILE=|PATH=|NO_COLOR=|COLUMNS=|TMPDIR=)`)

// Env is the environment for csquad and the fake: the process environment
// without anything of the developer's Claude Code session or provider, plus
// the temporary homes.
func (h *Home) Env(extra ...string) []string {
	var env []string
	for _, kv := range os.Environ() {
		if !providerVar.MatchString(kv) {
			env = append(env, kv)
		}
	}
	env = append(env, "HOME="+h.Home, "USERPROFILE="+h.Home, "CSQUAD_HOME="+h.Csquad, "CLAUDE_CONFIG_DIR="+h.Claude,
		"PATH="+h.Bin+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_STATE="+h.State, "TMPDIR="+h.Root)
	env = append(env, h.ExtraEnv...)
	return append(env, extra...)
}

// SetSessions replaces the fake's session list.
func (h *Home) SetSessions(list []Session) {
	if list == nil {
		list = []Session{}
	}
	writeState(h.State, list)
}

// Sessions reads the fake's session list.
func (h *Home) Sessions() []Session {
	list, _ := readState(h.State)
	return list
}

// Argv returns every argv the fake recorded.
func (h *Home) Argv() [][]string {
	b, _ := os.ReadFile(h.State + ".argv")
	var out [][]string
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var a []string
		if json.Unmarshal([]byte(line), &a) == nil {
			out = append(out, a)
		}
	}
	return out
}

// LastArgv is the last recorded argv.
func (h *Home) LastArgv() []string {
	all := h.Argv()
	if len(all) == 0 {
		return nil
	}
	return all[len(all)-1]
}

// ResetArgv forgets the recorded argvs.
func (h *Home) ResetArgv() { os.Remove(h.State + ".argv") }

// Calls is how often `claude agents` ran.
func (h *Home) Calls() int {
	st, err := os.Stat(h.State + ".calls")
	if err != nil {
		return 0
	}
	return int(st.Size())
}

// Task returns a fake background session named like a task.
func Task(n int, title, state string) Session {
	id := fmt.Sprintf("%08x", 0xaaaa0000+n)
	return Session{ID: id, SessionID: id + "-0000-4000-8000-000000000000", Kind: "background", Name: fmt.Sprintf("T%d · %s", n, title), State: state, Cwd: "/", StartedAt: time.Now().UnixMilli()}
}

// Arg returns the value after flag in argv.
func Arg(argv []string, flag string) string { return arg(argv, flag) }
