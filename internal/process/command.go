package process

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// Command creates a helper process whose entire process group is killed on context cancellation.
// Callers own Start/Wait and the context deadline. Interactive agent sessions must
// use their lifecycle runner instead: this helper does not track resumable sessions.
func Command(ctx context.Context, cwd string, env map[string]string, name string, args ...string) *exec.Cmd {
	c := exec.CommandContext(ctx, name, args...)
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return syscall.Kill(-c.Process.Pid, syscall.SIGKILL) }
	c.WaitDelay = time.Second
	c.Dir = cwd
	c.Env = agentenv.Environ(env)
	return c
}

// Run executes a helper with the current environment and a 30-second timeout.
func Run(cwd, name string, args ...string) (string, error) {
	return RunEnv(cwd, nil, name, args...)
}

// RunEnv executes a helper with environment overrides and a 30-second timeout.
// Success returns trimmed combined output; failure retains raw output and wraps
// the execution error so callers can inspect its exit status.
func RunEnv(cwd string, env map[string]string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := Command(ctx, cwd, env, name, args...)
	b, e := c.CombinedOutput()
	if e != nil {
		return string(b), fmt.Errorf("%s: %w: %s", name, e, strings.TrimSpace(string(b)))
	}
	return strings.TrimSpace(string(b)), nil
}

// RunStdoutEnv is for helpers whose stdout is a structured response. It uses
// the same timeout and process-group cancellation as RunEnv, but keeps stderr
// out of a successful response while retaining it in failure diagnostics.
func RunStdoutEnv(cwd string, env map[string]string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c := Command(ctx, cwd, env, name, args...)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	b, e := c.Output()
	if e != nil {
		return string(b), fmt.Errorf("%s: %w: %s", name, e, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(string(b)), nil
}
