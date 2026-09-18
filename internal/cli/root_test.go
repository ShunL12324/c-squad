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
		{args: []string{"--team", "/tmp/team", "member", "add", "alice", "--env", "A=a,b", "--env", "B=x=y", "--role", "reviewer"}, path: []string{"member", "add", "alice"}, values: map[string]string{"team": "/tmp/team", "env": "A=a,b\x00B=x=y", "role": "reviewer"}},
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
