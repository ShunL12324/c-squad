package cli

import (
	"bytes"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/squad"
)

func TestSyntaxErrorsNeverReachTeamOperations(t *testing.T) {
	cases := [][]string{
		{"resume", "folio", "--name", "other"},
		{"resume", "folio", "--team", "/tmp/team"},
		{"task", "create", "test"},
		{"member", "add"},
		{"start", "--engnie", "claude"},
		{"start", "--engine", "typo"},
		{"start", "--detach=typo"},
		{"--generation", "-1", "board"},
		{"message", "send", "alice", "--text", ""},
		{"message", "broadcast", "--text", "hello"},
		{"message", "broadcast", "--text", "hello", "--task", "T1", "--all"},
		{"task", "evidence", "T1", "--kind", "review", "--sha", "abc", "--passed", "typo", "--summary", "checked"},
		{"member", "missing"},
		{"board", "unexpected"},
		{"navigate", "--client", "/dev/pts/1", "--direction", "left"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := newCommand(func([]string, map[string]string, []string) error {
				t.Fatal("invalid invocation reached backend")
				return nil
			})
			root.SetArgs(args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid invocation")
			}
		})
	}
}

func TestParsedValuesPreserveOpaqueTextAndNativeArguments(t *testing.T) {
	tests := []struct {
		args, path, engine []string
		values             map[string]string
	}{
		{args: []string{"resume", "folio", "--detach"}, path: []string{"resume"}, values: map[string]string{"name": "folio", "detach": "true"}},
		{args: []string{"--team", "/tmp/team", "member", "add", "alice", "--instructions", "a,b=c"}, path: []string{"member", "add", "alice"}, values: map[string]string{"team": "/tmp/team", "instructions": "a,b=c"}},
		{args: []string{"task", "create", "--acceptance", "done", "--", "--literal-title"}, path: []string{"task", "create", "--literal-title"}, values: map[string]string{"acceptance": "done"}},
		{args: []string{"--name", "demo", "--detach"}, path: []string{"start"}, values: map[string]string{"name": "demo", "detach": "true"}},
		{args: []string{"run-engine", "--", "claude", "--settings", "{\"x\":1}", "--help"}, path: []string{"run-engine"}, engine: []string{"claude", "--settings", "{\"x\":1}", "--help"}, values: map[string]string{}},
	}
	for _, test := range tests {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			called := false
			root := newCommand(func(path []string, values map[string]string, engine []string) error {
				called = true
				if !reflect.DeepEqual(path, test.path) || !reflect.DeepEqual(values, test.values) || !reflect.DeepEqual(engine, test.engine) {
					t.Fatalf("unexpected invocation: %#v %#v %#v", path, values, engine)
				}
				return nil
			})
			root.SetArgs(test.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("backend was not called")
			}
		})
	}
}

// Navigation bindings invoke the CLI, so accepted flag values must match the
// backend protocol rather than the labels printed in the shortcut hint.
func TestNavigationBindingsReachBackend(t *testing.T) {
	for _, flags := range [][]string{
		{"--direction", "next"},
		{"--direction", "previous"},
		{"--index", "1"},
	} {
		t.Run(strings.Join(flags, " "), func(t *testing.T) {
			called := false
			root := newCommand(func(path []string, values map[string]string, _ []string) error {
				called = true
				if !reflect.DeepEqual(path, []string{"navigate"}) || values["client"] != "/dev/pts/1" || values[strings.TrimPrefix(flags[0], "--")] != flags[1] {
					t.Fatalf("unexpected navigation: %v %v", path, values)
				}
				return nil
			})
			args := []string{"--member", "master", "--generation", "0", "navigate", "--client", "/dev/pts/1"}
			root.SetArgs(append(args, flags...))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("navigation did not reach the backend")
			}
		})
	}
}

func TestHelpAndCompletionDoNotRequireRuntime(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	for _, args := range [][]string{{"--help"}, {"task", "create", "--help"}, {"help", "--help"}, {"usage", "member", "add"}, {"completion", "bash"}, {"completion", "zsh"}, {"completion", "fish"}, {"__complete", "start", "--engine", ""}} {
		root := newCommand(func([]string, map[string]string, []string) error {
			t.Fatal("help/completion invoked runtime")
			return nil
		})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if out.Len() == 0 {
			t.Fatalf("no output for %v", args)
		}
		if args[0] == "__complete" && (!strings.Contains(out.String(), "claude") || !strings.Contains(out.String(), "codex")) {
			t.Fatalf("missing engines: %s", out.String())
		}
	}
}

func TestErrorPresentationPreservesClassification(t *testing.T) {
	var out bytes.Buffer
	err := fmt.Errorf("resume: %w", squad.ErrTeamStopped)
	if code := Report(&out, err); code != 1 || !strings.Contains(out.String(), "resume --help") {
		t.Fatalf("%d %s", code, out.String())
	}
	out.Reset()
	usage := &usageError{errors.New("missing task"), "csquad task inspect"}
	if code := Report(&out, usage); code != 2 || !strings.Contains(out.String(), "task inspect --help") {
		t.Fatalf("%d %s", code, out.String())
	}
}

// A removed launch flag is explained before any other check can fail first,
// whatever its value, while doctor keeps its own --engine.
func TestRemovedLaunchFlagsAreExplainedFirst(t *testing.T) {
	for _, args := range [][]string{
		{"start", "--engine", "gpt"},
		{"member", "add", "x", "--engine", "codex"},
		{"member", "add", "x", "--model", ""},
		{"resume", "--env", "A=B"},
		{"resume", "--engine", "codex"},
		{"resume", "--model", "gpt-x"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := newCommand(func([]string, map[string]string, []string) error {
				t.Fatal("removed flag reached backend")
				return nil
			})
			root.SetArgs(args)
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "was removed") {
				t.Fatalf("got %v, want the removal explanation", err)
			}
		})
	}
	called := false
	root := newCommand(func([]string, map[string]string, []string) error { called = true; return nil })
	root.SetArgs([]string{"doctor", "--engine", "codex"})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err != nil || !called {
		t.Fatalf("doctor --engine was rejected: %v", err)
	}
}

// Help recommends one spelling per operation and leaves the runtime's bound
// identity selectors out, without starting anything. The hidden spellings and
// selectors still parse, so existing scripts and the runtime keep working.
func TestHelpRecommendsOneSpellingAndHidesIdentity(t *testing.T) {
	help := func(args ...string) string {
		t.Helper()
		root := newCommand(func([]string, map[string]string, []string) error {
			t.Fatal("help invoked runtime")
			return nil
		})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		root.SetArgs(append(args, "--help"))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	top := help()
	for _, hidden := range []string{"\n  start ", "\n  reply ", "\n  help ", "--member", "--generation", "--state-dir", "--team "} {
		if strings.Contains(top, hidden) {
			t.Fatalf("root help lists %q:\n%s", hidden, top)
		}
	}
	for _, shown := range []string{"\n  new ", "\n  question ", "\n  resume ", "--team-name"} {
		if !strings.Contains(top, shown) {
			t.Fatalf("root help lost %q:\n%s", shown, top)
		}
	}
	evidence := help("task", "evidence")
	for _, hidden := range []string{"--member", "--generation", "--state-dir", "--team "} {
		if strings.Contains(evidence, hidden) {
			t.Fatalf("task evidence help lists %q:\n%s", hidden, evidence)
		}
	}
	if !strings.Contains(evidence, "--submission") || !strings.Contains(evidence, "--team-name") {
		t.Fatalf("task evidence help lost its own flags:\n%s", evidence)
	}
	if message := help("message"); !strings.Contains(message, "\n  reply ") {
		t.Fatalf("message help lost reply:\n%s", message)
	}

	// Each hidden spelling reaches the backend exactly as the recommended one does.
	invocation := func(args ...string) string {
		t.Helper()
		var got string
		root := newCommand(func(path []string, values map[string]string, _ []string) error {
			got = fmt.Sprintf("%q %v", path, values)
			return nil
		})
		root.SetArgs(args)
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		if err := root.Execute(); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if got == "" {
			t.Fatalf("%v did not reach the backend", args)
		}
		return got
	}
	for _, pair := range [][2][]string{
		{{"start", "demo"}, {"new", "demo"}},
		{{"reply", "M1", "--text", "ok"}, {"message", "reply", "M1", "--text", "ok"}},
		{{"help", "request", "--text", "why"}, {"question", "request", "--text", "why"}},
		{{"help", "answer", "Q1", "--text", "yes"}, {"question", "answer", "Q1", "--text", "yes"}},
		{{"--state-dir", "/tmp/team", "--member", "a", "--generation", "2", "board"}, {"--team", "/tmp/team", "--member", "a", "--generation", "2", "board"}},
	} {
		if hidden, shown := invocation(pair[0]...), invocation(pair[1]...); hidden != shown {
			t.Fatalf("%v reached %s, but %v reached %s", pair[0], hidden, pair[1], shown)
		}
	}
	if got := invocation("--state-dir", "/tmp/team", "--member", "a", "--generation", "2", "board"); !strings.Contains(got, "generation:2") || !strings.Contains(got, "member:a") || !strings.Contains(got, "team:/tmp/team") {
		t.Fatalf("hidden selectors were not passed on: %s", got)
	}
}
