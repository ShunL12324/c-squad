package squad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

func TestMemberCommandPath(t *testing.T) {
	st := testStore(t)
	dir := filepath.Join(t.TempDir(), "binary directory with spaces")
	must(t, os.MkdirAll(dir, 0700))
	selected := filepath.Join(dir, "csquad-v2")
	must(t, os.WriteFile(selected, []byte("#!/bin/sh\nprintf 'selected:%s' \"$1\"\n"), 0700))
	other := t.TempDir()
	must(t, os.WriteFile(filepath.Join(other, "csquad"), []byte("#!/bin/sh\necho wrong\n"), 0700))
	t.Setenv("PATH", other)
	for _, override := range []map[string]string{nil, {"PATH": other}, {"PATH": ""}} {
		s := &State{Executable: selected}
		m := &Member{ID: "a", Generation: 1, Env: override}
		env, err := st.memberEnvironment(s, m)
		must(t, err)
		cmd := exec.Command("/bin/sh", "-c", "csquad 'argument with spaces'")
		cmd.Dir = t.TempDir()
		cmd.Env = agentenv.Environ(env)
		out, err := cmd.CombinedOutput()
		must(t, err)
		if string(out) != "selected:argument with spaces" {
			t.Fatalf("wrong command or arguments: %s", out)
		}
		if override != nil && override["PATH"] != other && override["PATH"] != "" {
			t.Fatal("member environment mutated")
		}
		// The runner receives the already-prepended PATH from tmux.
		t.Setenv("PATH", env["PATH"])
		again, err := st.memberEnvironment(s, m)
		must(t, err)
		if again["PATH"] != env["PATH"] {
			t.Fatalf("PATH grew on runner reentry: %q != %q", again["PATH"], env["PATH"])
		}
	}
}

func TestRunEngineCommandPathAcrossGenerations(t *testing.T) {
	for _, engine := range []config.Engine{config.Codex, config.Claude} {
		t.Run(string(engine), func(t *testing.T) {
			st := testStore(t)
			var previousLink string
			for generation := 1; generation <= 2; generation++ {
				gen := strconv.Itoa(generation)
				selected := filepath.Join(t.TempDir(), "selected version "+gen)
				must(t, os.WriteFile(selected, []byte("#!/bin/sh\nprintf '%s|%s|%s|%s' "+shellQuote(gen)+" \"$CSQUAD_MEMBER_ID\" \"$CSQUAD_GENERATION\" \"$1\"\n"), 0700))
				output := filepath.Join(t.TempDir(), "output")
				must(t, st.update(func(s *State) error {
					s.Executable = selected
					m := s.Members["a"]
					m.Engine, m.Generation, m.Cwd = engine, generation, t.TempDir()
					m.Env = map[string]string{"PATH": "/usr/bin:/bin"}
					return nil
				}))
				bindSession(t, st, "a", gen)
				must(t, runEngine(st, "a", generation, []string{"/bin/sh", "-c", "csquad board > " + shellQuote(output)}))
				out, err := os.ReadFile(output)
				must(t, err)
				if string(out) != gen+"|a|"+gen+"|board" {
					t.Fatalf("incorrect executable or lost identity: %s", out)
				}
				link := filepath.Join(st.Dir, "runtime", "a", gen, "bin", "csquad")
				if previousLink != "" {
					target, err := os.Readlink(previousLink)
					must(t, err)
					if target == selected {
						t.Fatal("restart retargeted previous generation")
					}
				}
				previousLink = link
			}
			if err := runEngine(st, "a", 1, []string{"/bin/sh", "-c", "exit 99"}); err != ErrStaleGeneration {
				t.Fatalf("stale generation allowed: %v", err)
			}
		})
	}
}

func TestMemberCommandPathRejectsRelativeExecutable(t *testing.T) {
	_, err := testStore(t).memberEnvironment(&State{Executable: "csquad"}, &Member{ID: "a", Generation: 1})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative executable accepted: %v", err)
	}
}
