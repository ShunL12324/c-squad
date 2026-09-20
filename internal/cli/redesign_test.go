package cli

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/squad"
)

func TestAdditiveGrammar(t *testing.T) {
	cases := []struct {
		args, path []string
		values     map[string]string
	}{
		{[]string{"start", "demo", "--detach"}, []string{"start"}, map[string]string{"name": "demo", "detach": "true"}},
		{[]string{"stop", "demo"}, []string{"stop"}, map[string]string{"name": "demo"}},
		{[]string{"recover", "demo", "--fresh"}, []string{"recover"}, map[string]string{"name": "demo", "fresh": "true"}},
		{[]string{"--team-name", "demo", "member", "attach", "alice"}, []string{"member", "attach", "alice"}, map[string]string{"team-name": "demo"}},
		{[]string{"--state-dir", "/tmp/team", "task", "list", "--output", "table"}, []string{"task", "list"}, map[string]string{"team": "/tmp/team", "output": "table"}},
		{[]string{"question", "answer", "Q1", "--text", "yes"}, []string{"help", "answer", "Q1"}, map[string]string{"text": "yes"}},
		{[]string{"question", "list"}, []string{"help", "list"}, map[string]string{}},
		{[]string{"message", "reply", "M1", "--text", "yes"}, []string{"reply", "M1"}, map[string]string{"text": "yes"}},
		{[]string{"_internal", "shutdown", "--epoch", "1", "--expected-generation", "2"}, []string{"shutdown"}, map[string]string{"epoch": "1", "expected-generation": "2"}},
		{[]string{"task", "evidence", "T1", "--submission", "S1", "--kind", "review", "--passed", "true", "--summary", "ok"}, []string{"task", "evidence", "T1"}, map[string]string{"submission": "S1", "kind": "review", "passed": "true", "summary": "ok"}},
		{[]string{"team", "remove", "demo", "--dry-run"}, []string{"team", "remove"}, map[string]string{"name": "demo", "dry-run": "true"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			called := false
			root := newCommand(func(path []string, values map[string]string, native []string) error {
				called = true
				if !reflect.DeepEqual(path, tc.path) || !reflect.DeepEqual(values, tc.values) || len(native) > 0 {
					t.Fatalf("got %v %v %v", path, values, native)
				}
				return nil
			})
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("not dispatched")
			}
		})
	}
}

func TestFileInputPreservesBytes(t *testing.T) {
	text := "literal `command` $(value)\nsecond line\tend\n"
	filename := filepath.Join(t.TempDir(), "input file.txt")
	if err := os.WriteFile(filename, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		args []string
		key  string
	}{
		{[]string{"message", "send", "alice", "--text-file", filename}, "text"},
		{[]string{"question", "request", "--text-file", "-"}, "text"},
		{[]string{"task", "progress", "T1", "--text-file", "-"}, "text"},
		{[]string{"task", "submit", "T1", "--summary-file", filename}, "summary"},
		{[]string{"member", "add", "alice", "--instructions-file", filename}, "instructions"},
		{[]string{"task", "create", "title", "--acceptance", "done", "--description-file", "-"}, "description"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			called := false
			root := newCommand(func(_ []string, values map[string]string, _ []string) error {
				called = true
				if values[tc.key] != text {
					t.Fatalf("changed input bytes: %q", values[tc.key])
				}
				if _, ok := values[tc.key+"-file"]; ok {
					t.Fatal("file flag leaked to runtime")
				}
				return nil
			})
			root.SetIn(strings.NewReader(text))
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("not dispatched")
			}
		})
	}
}

func TestNewSyntaxFailsBeforeDispatch(t *testing.T) {
	for _, args := range [][]string{
		{"start", "demo", "--name", "demo"}, {"start", "--team-name", "demo"}, {"--state-dir", "/tmp/a", "--team", "/tmp/a", "board"},
		{"--team-name", "demo", "stop", "demo"}, {"recover", "demo", "--state-dir", "/tmp/a"}, {"--team-name", "", "task", "list"},
		{"message", "send", "alice", "--text", "a", "--text-file", "-"}, {"message", "send", "alice", "--text-file", ""},
		{"message", "send", "alice", "--text-file", "/nonexistent/t24/input"}, {"task", "submit", "T1", "--summary-file", "-"},
		{"member", "add", "alice", "--instructions", "inline", "--instructions-file", "-"},
		{"task", "list", "--output", "xml"}, {"_internal", "task", "list"}, {"team", "remove", "demo", "--team-name", "other"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := newCommand(func([]string, map[string]string, []string) error {
				t.Fatal("invalid invocation dispatched")
				return nil
			})
			root.SetIn(strings.NewReader(" \n"))
			root.SetOut(&bytes.Buffer{})
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(args)
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid syntax")
			}
		})
	}
}

func TestRejectsMultipleStdinFieldsBeforeReading(t *testing.T) {
	values := map[string]string{"text-file": "-", "summary-file": "-"}
	root := newCommand(nil)
	root.SetIn(failReader{t})
	args := []string{}
	if err := normalizeInputs(root, []string{"task"}, values, &args); err == nil {
		t.Fatal("accepted two stdin fields")
	}
}

type failReader struct{ t *testing.T }

func (r failReader) Read([]byte) (int, error) {
	r.t.Fatal("read stdin before validation")
	return 0, nil
}

func TestAliasIdentityFences(t *testing.T) {
	t.Setenv("CSQUAD_STATE_DIR", "/bound/team")
	t.Setenv("CSQUAD_MEMBER_ID", "alice")
	t.Setenv("CSQUAD_GENERATION", "1")
	for _, args := range [][]string{
		{"--state-dir", "/other/team", "task", "list"},
		{"--member", "master", "question", "list"},
		{"--generation", "0", "message", "reply", "M1", "--text", "reply"},
		{"--member", "master", "member", "attach", "bob"},
	} {
		root := newCommand(squad.Execute)
		root.SetArgs(args)
		if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "conflicts with this session") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestInternalNativeArgumentsRemainOpaque(t *testing.T) {
	root := newCommand(func(path []string, values map[string]string, native []string) error {
		if !reflect.DeepEqual(path, []string{"run-engine"}) || len(values) != 0 || !reflect.DeepEqual(native, []string{"codex", "--help", "--team-name", "literal"}) {
			t.Fatalf("%v %v %v", path, values, native)
		}
		return nil
	})
	root.SetArgs([]string{"_internal", "run-engine", "--", "codex", "--help", "--team-name", "literal"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestCompletionUsesExplicitSelectors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CSQUAD_HOME", home)
	t.Setenv("CSQUAD_STATE_DIR", "")
	dir := filepath.Join(home, "teams", "demo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`CREATE TABLE state (id INTEGER PRIMARY KEY, data TEXT); INSERT INTO state VALUES (1, '{"id":"demo","tasks":{"T_named":{"id":"T_named"}},"members":{"alice":{"id":"alice"}}}')`); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	for _, selector := range [][]string{{"--team-name", "demo"}, {"--state-dir", dir}, {"--team", dir}} {
		root := newCommand(func([]string, map[string]string, []string) error {
			t.Fatal("completion dispatched runtime")
			return nil
		})
		var out bytes.Buffer
		root.SetOut(&out)
		root.SetErr(&bytes.Buffer{})
		args := append([]string{"__complete"}, selector...)
		root.SetArgs(append(args, "task", "inspect", ""))
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(out.String(), "T_named") {
			t.Fatalf("%v: %s", selector, out.String())
		}
	}
}
