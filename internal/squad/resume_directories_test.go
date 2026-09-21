package squad

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResumeDirectoryValidationAndRepair(t *testing.T) {
	st := testStore(t)
	good := t.TempDir()
	file := filepath.Join(good, "file")
	must(t, os.WriteFile(file, nil, 0600))
	must(t, st.update(func(s *State) error {
		s.Active = false
		s.Members["master"].Cwd = good
		s.Members["a"].Cwd = filepath.Join(good, "missing")
		s.Members["b"].Cwd = file
		return nil
	}))
	s, err := st.read()
	must(t, err)
	err = validateResumeDirectories(s)
	if err == nil || !strings.Contains(err.Error(), "member a") || !strings.Contains(err.Error(), "member b") || !strings.Contains(err.Error(), "set-cwd") {
		t.Fatalf("incomplete diagnosis: %v", err)
	}
	if err = setStoppedMemberDirectory(st, "a", "a", good); err == nil {
		t.Fatal("worker repaired directory")
	}
	must(t, Execute([]string{"member", "set-cwd", "a"}, options{"team": st.Dir, "cwd": good}, nil))
	must(t, setStoppedMemberDirectory(st, "master", "b", good))
	s, err = st.read()
	must(t, err)
	must(t, validateResumeDirectories(s))
	if s.Active || s.Members["a"].Generation != 1 {
		t.Fatal("directory repair started or reincarnated member")
	}
	must(t, st.update(func(s *State) error { s.Active = true; return nil }))
	if err = setStoppedMemberDirectory(st, "master", "a", good); err == nil {
		t.Fatal("changed running cwd")
	}
}

func TestResumeInvalidDirectoryLeavesLedgerUntouched(t *testing.T) {
	st := testStore(t)
	t.Setenv("CSQUAD_CONFIG", filepath.Join(t.TempDir(), "config.toml"))
	must(t, st.update(func(s *State) error {
		s.Active = false
		for _, m := range s.Members {
			m.Cwd = t.TempDir()
		}
		s.Members["a"].Cwd = filepath.Join(t.TempDir(), "missing")
		return nil
	}))
	before, err := st.read()
	must(t, err)
	err = resumeTeam(st, options{})
	if err == nil || !strings.Contains(err.Error(), "invalid working directories") {
		t.Fatalf("resume: %v", err)
	}
	after, err := st.read()
	must(t, err)
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after)
	if string(a) != string(b) {
		t.Fatal("failed preflight mutated team state")
	}
}
