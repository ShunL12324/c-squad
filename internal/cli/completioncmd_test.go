package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	root := newCommand(func([]string, map[string]string, []string) error {
		t.Fatal("completion setup reached the team backend")
		return nil
	})
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	if err := root.Execute(); err != nil {
		t.Fatalf("%v: %v\n%s", args, err, out.String())
	}
	return out.String()
}

// Repeated installs must be safe to paste into instructions, and the installed
// file must be exactly what the generator prints.
func TestCompletionInstallIsIdempotent(t *testing.T) {
	for _, test := range []struct{ shell, file string }{
		{"bash", "csquad"},
		{"zsh", "_csquad"},
		{"fish", "csquad.fish"},
		{"powershell", "csquad.ps1"},
	} {
		t.Run(test.shell, func(t *testing.T) {
			directory := filepath.Join(t.TempDir(), "nested", "completions")
			path := filepath.Join(directory, test.file)
			if out := runCLI(t, "completion", "install", "--shell", test.shell, "--dir", directory); !strings.HasPrefix(out, "Installed "+test.shell+" completion: "+path) {
				t.Fatalf("first install: %s", out)
			}
			if out := runCLI(t, "completion", "install", "--shell", test.shell, "--dir", directory); !strings.HasPrefix(out, "Unchanged "+test.shell+" completion: "+path) {
				t.Fatalf("second install: %s", out)
			}
			installed, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if generated := runCLI(t, "completion", test.shell); string(installed) != generated {
				t.Fatal("installed script differs from the generated script")
			}
			if err := os.WriteFile(path, []byte("stale\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			if out := runCLI(t, "completion", "install", "--shell", test.shell, "--dir", directory); !strings.HasPrefix(out, "Updated "+test.shell+" completion: "+path) {
				t.Fatalf("refresh: %s", out)
			}
			entries, err := os.ReadDir(directory)
			if err != nil {
				t.Fatal(err)
			}
			// A staged temporary file must never survive a completed install.
			if len(entries) != 1 || entries[0].Name() != test.file {
				t.Fatalf("unexpected directory content: %v", entries)
			}
		})
	}
}

// Supplementary format check: the shell-level proof lives in
// scripts/test-npm.py, which runs these lines in Zsh and Bash. A path the user
// pastes unquoted splits into several words and kills completion silently.
func TestCompletionInstallQuotesPastedPaths(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "dir with spaces")
	for _, test := range []struct{ shell, want string }{
		{"zsh", "\n\tfpath=('" + directory + "' $fpath)\n"},
		{"bash", "\n\tsource '" + filepath.Join(directory, "csquad") + "'"},
		{"powershell", "\n\t. '" + filepath.Join(directory, "csquad.ps1") + "'"},
	} {
		t.Run(test.shell, func(t *testing.T) {
			if out := runCLI(t, "completion", "install", "--shell", test.shell, "--dir", directory); !strings.Contains(out, test.want) {
				t.Fatalf("missing %q in: %s", test.want, out)
			}
		})
	}
}

// The installer owns one directory; shell configuration stays the user's.
func TestCompletionInstallTouchesNoShellConfiguration(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("# untouched\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	out := runCLI(t, "completion", "install", "--shell", "zsh")
	if !strings.Contains(out, filepath.Join(home, "data", "zsh", "site-functions", "_csquad")) {
		t.Fatalf("unexpected target: %s", out)
	}
	content, err := os.ReadFile(rc)
	if err != nil || string(content) != "# untouched\n" {
		t.Fatalf("shell configuration was modified: %q %v", content, err)
	}
	// The user has to add the fpath entry, so the command must print it.
	if !strings.Contains(out, "fpath=('"+filepath.Join(home, "data", "zsh", "site-functions")+"' $fpath)") {
		t.Fatalf("missing fpath instruction: %s", out)
	}
}

func TestCompletionStatusReportsFileState(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	if out := runCLI(t, "completion", "status"); !strings.Contains(out, "zsh         missing") {
		t.Fatalf("expected a missing zsh file: %s", out)
	}
	runCLI(t, "completion", "install", "--shell", "zsh")
	if out := runCLI(t, "completion", "status"); !strings.Contains(out, "zsh         current") {
		t.Fatalf("expected a current zsh file: %s", out)
	}
	path := filepath.Join(home, "data", "zsh", "site-functions", "_csquad")
	if err := os.WriteFile(path, []byte("#compdef csquad\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runCLI(t, "completion", "status")
	if !strings.Contains(out, "zsh         differs") {
		t.Fatalf("expected a differing zsh file: %s", out)
	}
	// Status must hand over the in-shell check instead of claiming to know it.
	if !strings.Contains(out, "print -r -- ${_comps[csquad]:-missing}") {
		t.Fatalf("missing in-shell check: %s", out)
	}
}

// Issue #2: the stock Cobra help sends every user to Homebrew.
func TestCompletionHelpDocumentsInstallWithoutHomebrewCommands(t *testing.T) {
	for _, args := range [][]string{
		{"completion", "--help"},
		{"completion", "bash", "--help"},
		{"completion", "zsh", "--help"},
		{"completion", "fish", "--help"},
		{"completion", "powershell", "--help"},
		{"completion", "install", "--help"},
		{"completion", "status", "--help"},
	} {
		out := runCLI(t, args...)
		for _, forbidden := range []string{"brew --prefix", "brew install", "$(brew"} {
			if strings.Contains(out, forbidden) {
				t.Fatalf("%v documents a Homebrew command: %s", args, out)
			}
		}
		if !strings.Contains(out, "csquad completion install") && !strings.Contains(out, "install the completion script") {
			t.Fatalf("%v does not point at the installer: %s", args, out)
		}
	}
}

func TestCompletionInstallRejectsUnknownShell(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/nushell")
	root := newCommand(func([]string, map[string]string, []string) error { return nil })
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"completion", "install", "--dir", t.TempDir()})
	err := root.Execute()
	if err == nil {
		t.Fatal("accepted an unsupported shell")
	}
	if !strings.Contains(err.Error(), "--shell") {
		t.Fatalf("unhelpful error: %v", err)
	}
}
