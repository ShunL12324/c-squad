package agentenv

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Validate rejects overrides that could forge team identity or tmux routing.
// Values may be empty but must not contain NUL, which cannot appear in exec environments.
func Validate(env map[string]string) error {
	for k, v := range env {
		if !envKey.MatchString(k) || strings.HasPrefix(k, "CSQUAD_") || k == "TMUX" || k == "TMUX_PANE" {
			return fmt.Errorf("invalid or reserved environment key %q", k)
		}
		if strings.ContainsRune(v, 0) {
			return fmt.Errorf("environment %s contains NUL", k)
		}
	}
	return nil
}

// Parse decodes NUL-separated KEY=VALUE arguments, with the last value winning.
// NUL is the CLI parser separator; values may contain spaces and equals signs.
func Parse(raw string) (map[string]string, error) {
	out := map[string]string{}
	for _, item := range strings.Split(raw, "\x00") {
		if item == "" {
			continue
		}
		k, v, ok := strings.Cut(item, "=")
		if !ok {
			return nil, fmt.Errorf("--env requires KEY=VALUE")
		}
		out[k] = v
	}
	return out, Validate(out)
}

// Merge overlays layers from left to right into a new map without mutating inputs.
func Merge(layers ...map[string]string) map[string]string {
	out := map[string]string{}
	for _, layer := range layers {
		for k, v := range layer {
			out[k] = v
		}
	}
	return out
}

// SnapshotSelectors freezes engine configuration directories at team startup.
// An empty CLAUDE_CONFIG_DIR records an unset variable, not an explicit default path:
// Claude treats an explicit directory differently when locating its global settings.
func SnapshotSelectors() map[string]string {
	inherited := map[string]string{}
	// Freeze account selectors so a different tmux server or later invoker
	// cannot silently change the account. Other ambient environment is inherited.
	for _, k := range []string{"CODEX_HOME", "CLAUDE_CONFIG_DIR"} {
		if v, ok := os.LookupEnv(k); ok {
			inherited[k] = v
		} else if home, e := os.UserHomeDir(); e == nil {
			if k == "CODEX_HOME" {
				inherited[k] = home + "/.codex"
			} else {
				// Explicit default CLAUDE_CONFIG_DIR changes Claude's global JSON location.
				inherited[k] = ""
			}
		}
	}
	return inherited
}

// Environ overlays the current process environment for a native engine invocation.
// Empty CLAUDE_CONFIG_DIR removes the variable; other empty overrides remain present.
func Environ(overrides map[string]string) []string {
	out := []string{}
	for _, entry := range os.Environ() {
		k, _, _ := strings.Cut(entry, "=")
		if _, ok := overrides[k]; !ok {
			out = append(out, entry)
		}
	}
	for k, v := range overrides {
		if k == "CLAUDE_CONFIG_DIR" && v == "" {
			continue
		}
		out = append(out, k+"="+v)
	}
	return out
}
