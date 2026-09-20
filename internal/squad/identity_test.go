package squad

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

// bindSession reproduces the variables engine.go exports into a member pane.
func bindSession(t *testing.T, st *Store, member, generation string) {
	t.Helper()
	t.Setenv("CSQUAD_STATE_DIR", st.Dir)
	t.Setenv("CSQUAD_MEMBER_ID", member)
	t.Setenv("CSQUAD_GENERATION", generation)
}

func conflicted(e error) bool {
	if e == nil {
		return false
	}
	return strings.Contains(e.Error(), "conflicts with this session") || strings.Contains(e.Error(), "generation 0 is not accepted")
}

// The plumbing exemption is load-bearing. panels.go, navigation.go, recovery.go
// and pump.go run "--member master --generation 0" through tmux run-shell in a
// pane whose environment names the member who owns it, so the identity rules
// must not apply there. Each case below is invoked with a downstream argument
// that reaches the command body and stops immediately, so a pass proves the
// call got past identity resolution rather than merely avoiding one error.
// Remove plumbingCommands and every case fails with an identity conflict.
func TestPlumbingKeepsFlagAuthorityInsideMemberSession(t *testing.T) {
	for _, tt := range []struct {
		name string
		path []string
		opts options
		want string
	}{
		{"navigate", []string{"navigate"}, options{"client": ""}, "--client required"},
		{"ui-panel", []string{"ui-panel"}, options{"owner": "a", "view": "bogus"}, "panel must be members or tasks"},
		{"shutdown", []string{"shutdown"}, options{"epoch": "99", "expected-generation": "99"}, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := testStore(t)
			bindSession(t, st, "a", "1")
			o := options{"team": st.Dir, "member": "master", "generation": "0"}
			for k, v := range tt.opts {
				o[k] = v
			}
			e := Execute(tt.path, o, nil)
			if conflicted(e) {
				t.Fatalf("plumbing was subjected to the ledger identity rules: %v", e)
			}
			if tt.want == "" {
				if e != nil {
					t.Fatalf("want the command body to complete, got %v", e)
				}
				return
			}
			if e == nil || !strings.Contains(e.Error(), tt.want) {
				t.Fatalf("want the command body to report %q, got %v", tt.want, e)
			}
		})
	}
}

// A member session must not be able to act as another member or another
// incarnation. Before this rule the flag silently won over the environment.
func TestLedgerCommandRefusesContradictingIdentity(t *testing.T) {
	for _, tt := range []struct {
		name, flag, value string
		mentions          []string
	}{
		{"member", "member", "master", []string{"master", "a"}},
		{"generation", "generation", "7", []string{"7", "1"}},
		{"team", "team", "/nowhere/else", []string{"/nowhere/else"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			st := testStore(t)
			bindSession(t, st, "a", "1")
			o := options{tt.flag: tt.value}
			e := Execute([]string{"task", "list"}, o, nil)
			if e == nil {
				t.Fatal("a contradicting flag must be refused, not silently preferred")
			}
			if !strings.Contains(e.Error(), "conflicts with this session") {
				t.Fatalf("unexpected failure: %v", e)
			}
			for _, want := range tt.mentions {
				if !strings.Contains(e.Error(), want) {
					t.Fatalf("error must name both values, %q missing from %v", want, e)
				}
			}
		})
	}
}

// Teams started before this change inject the full three-flag prefix. Those
// values agree with the session, so they must keep working untouched.
func TestLedgerCommandAcceptsRepeatedIdentityFromOlderPrefix(t *testing.T) {
	st := testStore(t)
	bindSession(t, st, "a", "1")
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir, "member": "a", "generation": "1"}, nil); e != nil {
		t.Fatalf("the prefix injected by an already running team must keep working: %v", e)
	}
	// A trailing separator is the same directory, not a different team.
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir + string(filepath.Separator)}, nil); e != nil {
		t.Fatalf("equivalent team paths must agree: %v", e)
	}
}

// Generation 0 skips the stale-write check, which is correct only outside a
// member session.
func TestGenerationZeroOnlyOutsideMemberSession(t *testing.T) {
	t.Run("inside", func(t *testing.T) {
		st := testStore(t)
		bindSession(t, st, "a", "")
		e := Execute([]string{"task", "list"}, options{"generation": "0"}, nil)
		if e == nil || !strings.Contains(e.Error(), "generation 0 is not accepted") {
			t.Fatalf("want generation 0 refused inside a member session, got %v", e)
		}
	})
	t.Run("outside", func(t *testing.T) {
		st := testStore(t)
		if e := Execute([]string{"task", "list"}, options{"team": st.Dir, "generation": "0"}, nil); e != nil {
			t.Fatalf("an outside terminal has no incarnation and must keep generation 0: %v", e)
		}
	})
}

// Without the variables the CLI behaves exactly as before: flags apply and the
// caller defaults to master.
func TestExternalTerminalIdentityIsUnchanged(t *testing.T) {
	st := testStore(t)
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir}, nil); e != nil {
		t.Fatalf("default master identity regressed: %v", e)
	}
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir, "member": "a", "generation": "1"}, nil); e != nil {
		t.Fatalf("explicit flags must still select an identity: %v", e)
	}
	if e := Execute([]string{"task", "list"}, options{"team": st.Dir, "member": "ghost"}, nil); e == nil {
		t.Fatal("an unknown member must still be rejected")
	}
}

// namedTeamStore creates a team that namedTeam can resolve by name.
func namedTeamStore(t *testing.T, home, name string) *Store {
	t.Helper()
	st, e := openStore(filepath.Join(home, "teams", name))
	must(t, e)
	t.Cleanup(func() { st.DB.Close() })
	must(t, st.update(func(s *State) error {
		*s = State{Version: 1, ID: name, Root: t.TempDir(), Active: true, Members: map[string]*Member{}, Tasks: map[string]*Task{}, Questions: map[string]*Question{}}
		s.Members["master"] = &Member{ID: "master", Engine: config.Claude, State: MemberStateIdle, Generation: 1}
		return nil
	}))
	return st
}

// --name resolves a team by name, which escapes the --team agreement. Acting on
// another team from inside a member session cannot succeed, so it must say so
// with the workaround rather than fail later on an unrelated member lookup or,
// when both teams happen to share an identity and generation, quietly proceed.
func TestCrossTeamFromMemberSessionIsRefusedWithWorkaround(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CSQUAD_HOME", home)
	bound := namedTeamStore(t, home, "bound")
	namedTeamStore(t, home, "other")
	t.Setenv("CSQUAD_STATE_DIR", bound.Dir)
	t.Setenv("CSQUAD_MEMBER_ID", "master")
	t.Setenv("CSQUAD_GENERATION", "1")
	for _, path := range [][]string{{"board"}, {"stop"}, {"resume"}, {"ui"}} {
		e := Execute(path, options{"name": "other"}, nil)
		if e == nil || !strings.Contains(e.Error(), "bound to team") {
			t.Fatalf("%v --name other: want the binding refusal, got %v", path, e)
		}
		for _, want := range []string{`"bound"`, `"other"`, "outside the team"} {
			if !strings.Contains(e.Error(), want) {
				t.Fatalf("%v: refusal must name both teams and the workaround, %q missing from %v", path, want, e)
			}
		}
	}
	// Naming the team the session is already bound to is not a cross-team call.
	if e := Execute([]string{"board"}, options{"name": "bound"}, nil); e != nil && strings.Contains(e.Error(), "bound to team") {
		t.Fatalf("naming the session's own team must not be refused: %v", e)
	}
}

func TestCrossTeamFromExternalTerminalIsAllowed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CSQUAD_HOME", home)
	namedTeamStore(t, home, "other")
	if e := Execute([]string{"board"}, options{"name": "other"}, nil); e != nil && strings.Contains(e.Error(), "bound to team") {
		t.Fatalf("an outside terminal must reach any team: %v", e)
	}
}

func TestExecutablePathIsNormalised(t *testing.T) {
	npm := "/opt/lib/node_modules/csquad/bin/../native/darwin-arm64/csquad"
	if got := cleanPath(npm); got != "/opt/lib/node_modules/csquad/native/darwin-arm64/csquad" {
		t.Fatalf("npm launcher path not normalised: %s", got)
	}
	if cleanPath("") != "" {
		t.Fatal("an absent path must stay absent rather than become a dot")
	}
}

// The injected prefix is what an agent copies, so it must no longer repeat the
// identity flags and must say that a conflicting value is refused.
func TestInjectedPrefixDropsIdentityFlags(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		s.Executable = "/opt/csquad/native/csquad"
		s.Members["a"].Role = "reviewer"
		return nil
	}))
	s, e := st.read()
	must(t, e)
	text := prompt(s, s.Members["a"], config.Template{})
	// The old prefix bound concrete values; the instruction naming the flags as
	// forbidden must survive, so assert on the values rather than the flag names.
	for _, unwanted := range []string{"--team " + shellQuote(st.Dir), "--member " + shellQuote("a"), "--generation 1"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("injected prefix still carries %q", unwanted)
		}
	}
	for _, want := range []string{shellQuote("/opt/csquad/native/csquad") + " COMMAND", "bound to this session", "rejected"} {
		if !strings.Contains(text, want) {
			t.Fatalf("injected prefix missing %q", want)
		}
	}
}
