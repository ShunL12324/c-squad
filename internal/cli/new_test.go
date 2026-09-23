package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestNewCreationCompatibility(t *testing.T) {
	for _, prefix := range [][]string{{"new", "-s", "demo"}, {"new", "--name", "demo"}, {"new", "demo"}, {"start", "-s", "demo"}, {"start", "--name", "demo"}, {"start", "demo"}, {"-s", "demo"}} {
		t.Run(strings.Join(prefix, " "), func(t *testing.T) {
			called := false
			root := newCommand(func(path []string, values map[string]string, native []string) error {
				called = true
				want := map[string]string{"name": "demo", "profile": "work", "detach": "true"}
				if !reflect.DeepEqual(path, []string{"start"}) || !reflect.DeepEqual(values, want) || len(native) != 0 {
					t.Fatalf("dispatch: %v %v %v", path, values, native)
				}
				return nil
			})
			root.SetArgs(append(prefix, "--profile", "work", "--detach"))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !called {
				t.Fatal("not dispatched")
			}
		})
	}
	for _, args := range [][]string{nil, {"new"}, {"start"}} {
		root := newCommand(func(path []string, values map[string]string, _ []string) error {
			if !reflect.DeepEqual(path, []string{"start"}) || len(values) != 0 {
				t.Fatalf("%v %v", path, values)
			}
			return nil
		})
		root.SetArgs(args)
		if err := root.Execute(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNewInvalidCreation(t *testing.T) {
	for _, args := range [][]string{
		{"new", "-s"}, {"new", "-s", ""}, {"new", "demo", "-s", "other"},
		{"new", "--team-name", "demo"}, {"new", "--team", "/tmp/team"}, {"new", "--state-dir", "/tmp/team"},
		{"new", "one", "two"}, {"new", "--engine", "unknown"}, {"resume", "-s", "demo"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			root := newCommand(func([]string, map[string]string, []string) error { t.Fatal("invalid command dispatched"); return nil })
			root.SetArgs(args)
			if err := root.Execute(); err == nil {
				t.Fatal("accepted invalid command")
			}
		})
	}
}

func TestNewHelpAndCompletion(t *testing.T) {
	for _, tc := range []struct {
		args []string
		want string
	}{
		{[]string{"new", "--help"}, "-s, --name"},
		{[]string{"start", "--help"}, "-s, --name"},
		{[]string{"--help"}, "resume restores a stopped team"},
		{[]string{"__complete", "ne"}, "new\t"},
		{[]string{"__complete", "new", "--na"}, "--name\t"},
		{[]string{"__complete", "new", "--engine", "co"}, "codex"},
		{[]string{"__complete", "new", "-s", ""}, ":4"},
		{[]string{"__complete", "new", ""}, ":4"},
		{[]string{"completion", "bash"}, "__start_csquad"},
		{[]string{"completion", "zsh"}, "_csquad"},
		{[]string{"completion", "fish"}, "csquad"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := newCommand(func([]string, map[string]string, []string) error {
				t.Fatal("help/completion executed runtime")
				return nil
			})
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetErr(&bytes.Buffer{})
			root.SetArgs(tc.args)
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), tc.want) {
				t.Fatalf("missing %q: %s", tc.want, out.String())
			}
		})
	}
}
