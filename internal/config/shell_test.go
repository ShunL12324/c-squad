package config

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/process"
)

func TestShellAliasArgumentsExitAndCancellation(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			if _, err := exec.LookPath(shell); err != nil {
				t.Skip(shell + " unavailable")
			}
			home := t.TempDir()
			client := filepath.Join(home, "cfuse")
			capture := filepath.Join(home, "argv")
			must(t, os.WriteFile(client, []byte("#!/bin/sh\nprintf '%s\\0' \"$@\" > \"$CAPTURE\"\nif [ \"$2\" = wait ]; then sleep 30; else exit 23; fi\n"), 0700))
			// Paths from TempDir contain no single quotes; all other values below
			// are literal fixture text, never a real account or user shell file.
			rc := "alias customcc=\"FAKE_TOKEN='alias token' '" + client + "' --cc\"\n"
			must(t, os.WriteFile(filepath.Join(home, ".bashrc"), []byte(rc), 0600))
			must(t, os.WriteFile(filepath.Join(home, ".zshrc"), []byte(rc), 0600))
			command := Command{Shell: shell, Executable: "customcc"}
			env := map[string]string{"HOME": home, "ZDOTDIR": home, "CAPTURE": capture, "FAKE_TOKEN": "configured secret"}
			want := []string{"--cc", "argument with spaces", "", `quotes ' " $(touch never) ; &`}
			name, args, prepared := command.Invocation(env, "", want[1:]...)
			if strings.Contains(strings.Join(args, " "), "configured secret") {
				t.Fatal("environment secret appeared in argv")
			}
			_, err := process.RunEnv(home, prepared, name, args...)
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 23 {
				t.Fatalf("lost exit status: %v", err)
			}
			data, err := os.ReadFile(capture)
			must(t, err)
			if got := strings.Split(strings.TrimSuffix(string(data), "\x00"), "\x00"); !reflect.DeepEqual(got, want) {
				t.Fatalf("argv changed: %q", got)
			}
			name, args, prepared = command.Invocation(env, "", "wait")
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			started := time.Now()
			cmd := process.Command(ctx, home, prepared, name, args...)
			if err := cmd.Run(); err == nil || !errors.Is(ctx.Err(), context.DeadlineExceeded) {
				t.Fatalf("cancellation lost: %v", err)
			}
			if time.Since(started) > 3*time.Second {
				t.Fatal("alias child survived group cancellation")
			}
		})
	}
}
