package squad

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/process"
)

// Ask the engine to resolve its own configuration; don't reimplement its layer
// precedence or print configuration (which can contain credentials).
type codexSessionConfig struct {
	Instructions string                     `json:"developer_instructions"`
	Hooks        map[string]json.RawMessage `json:"hooks"`
	Warnings     []string                   `json:"-"`
}

func codexConfig(cwd string, trusted bool, environments ...map[string]string) (config codexSessionConfig, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	args := []string{"app-server"}
	if trusted {
		args = append(args, "-c", codexTrustOverride(cwd))
	}
	var env map[string]string
	if len(environments) > 0 {
		env = environments[0]
	}
	c := process.Command(ctx, cwd, env, "codex", args...)
	diagnostics := &codexDiagnostics{}
	c.Stderr = diagnostics
	in, e := c.StdinPipe()
	if e != nil {
		return codexSessionConfig{}, e
	}
	out, e := c.StdoutPipe()
	if e != nil {
		return codexSessionConfig{}, e
	}
	if e = c.Start(); e != nil {
		return codexSessionConfig{}, e
	}
	defer func() {
		timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
		_ = in.Close()
		cancel()
		_ = c.Wait()
		if err != nil {
			if timedOut {
				err = fmt.Errorf("codex configuration lookup timed out after 10s: %w", context.DeadlineExceeded)
			}
			err = fmt.Errorf("read Codex configuration in %q: %w", cwd, err)
			if text := diagnostics.String(); text != "" {
				err = fmt.Errorf("%w\nCodex: %s", err, text)
			}
		}
	}()
	enc := json.NewEncoder(in)
	if e = enc.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"clientInfo": map[string]string{"name": "csquad", "version": "0.1.0"}}}); e != nil {
		return codexSessionConfig{}, e
	}
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), 8<<20)
	for scan.Scan() {
		var v struct {
			ID     int             `json:"id"`
			Error  json.RawMessage `json:"error"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(scan.Bytes(), &v) != nil {
			continue
		}
		if v.ID == 1 {
			if len(v.Error) > 0 && string(v.Error) != "null" {
				return codexSessionConfig{}, codexRPCError("initialize", v.Error)
			}
			for _, request := range []any{
				map[string]any{"method": "initialized"},
				map[string]any{"id": 2, "method": "config/read", "params": map[string]any{"cwd": cwd, "includeLayers": true}},
			} {
				if e = enc.Encode(request); e != nil {
					return codexSessionConfig{}, e
				}
			}
			continue
		}
		if v.ID == 2 {
			if len(v.Error) > 0 && string(v.Error) != "null" {
				return codexSessionConfig{}, codexRPCError("config/read", v.Error)
			}
			var result struct {
				Config codexSessionConfig `json:"config"`
				Layers []struct {
					Name           json.RawMessage `json:"name"`
					DisabledReason string          `json:"disabledReason"`
				} `json:"layers"`
			}
			if e = json.Unmarshal(v.Result, &result); e != nil {
				return codexSessionConfig{}, fmt.Errorf("invalid Codex config/read response: %w", e)
			}
			for _, layer := range result.Layers {
				if layer.DisabledReason != "" {
					result.Config.Warnings = append(result.Config.Warnings, layer.DisabledReason)
				}
			}
			// Moving disabled hook handlers into session flags can change their
			// native identities. Refuse instead of accidentally re-enabling them.
			var states map[string]struct {
				Enabled  *bool `json:"enabled"`
				Disabled bool  `json:"disabled"`
			}
			if raw := result.Config.Hooks["state"]; len(raw) > 0 {
				if e = json.Unmarshal(raw, &states); e != nil {
					return codexSessionConfig{}, fmt.Errorf("unknown native hook state format: %w", e)
				}
				for _, state := range states {
					if state.Disabled || (state.Enabled != nil && !*state.Enabled) {
						return codexSessionConfig{}, fmt.Errorf("native disabled-hook overrides need explicit adapter support; refusing to re-enable them by session injection")
					}
				}
			}
			return result.Config, nil
		}
	}
	if err := scan.Err(); err != nil {
		return codexSessionConfig{}, fmt.Errorf("read effective Codex instructions: %w", err)
	}
	return codexSessionConfig{}, fmt.Errorf("codex app-server closed before config/read response")
}

// Encode the JSON-shaped hook configuration as TOML inline values without
// dropping the user's existing matcher groups when adding our session hook.
func tomlValue(v any) (string, error) {
	switch x := v.(type) {
	case string:
		b, _ := json.Marshal(x)
		return string(b), nil
	case bool:
		return strconv.FormatBool(x), nil
	case int:
		return strconv.Itoa(x), nil
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64), nil
	case []any:
		parts := []string{}
		for _, v := range x {
			p, e := tomlValue(v)
			if e != nil {
				return "", e
			}
			parts = append(parts, p)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := []string{}
		for k, v := range x {
			if v != nil {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		parts := []string{}
		for _, k := range keys {
			p, e := tomlValue(x[k])
			if e != nil {
				return "", e
			}
			key, _ := json.Marshal(k)
			parts = append(parts, string(key)+"="+p)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		return "", fmt.Errorf("unsupported hook configuration value %T", v)
	}
}

// Codex defers creating a thread until the first real user message. Bootstrap
// that message only in a verified, unattended empty composer. Subsequent
// messages use the native queue. Never type into an attached user's terminal.
func (st *Store) startCodexInput(s *State, m *Member, message string) error {
	clients, err := tm(s, "list-clients", "-F", "#{session_name}")
	if err != nil {
		return err
	}
	for _, session := range strings.Split(clients, "\n") {
		if session == m.Session {
			return fmt.Errorf("first message waiting: member has an attached client; enter a first message or detach")
		}
	}
	pane, err := tm(s, "capture-pane", "-p", "-t", agentPane(m))
	if err != nil {
		return err
	}
	if !codexEmptyComposer(pane) {
		return fmt.Errorf("first message waiting for native empty Codex composer")
	}
	// The JSON envelope escapes message control characters; use literal keys so
	// incoming task text cannot become tmux key names or shell commands.
	message = strings.ReplaceAll(message, "\n", " ")
	if _, err = tm(s, "send-keys", "-t", agentPane(m), "-l", "--", message); err != nil {
		return err
	}
	time.Sleep(100 * time.Millisecond)
	_, err = tm(s, "send-keys", "-t", agentPane(m), "Enter")
	return err
}

// codexEmptyComposer recognizes the empty native input near the bottom of the
// screen. Resumed conversations can scroll the startup banner out of view.
func codexEmptyComposer(pane string) bool {
	lines := strings.Split(strings.TrimRight(pane, "\n "), "\n")
	footer := lines[max(0, len(lines)-12):]
	if strings.Contains(strings.ToLower(strings.Join(footer, "\n")), "esc to interrupt") {
		return false
	}
	for i := len(footer) - 1; i >= 0; i-- {
		line := strings.TrimSpace(footer[i])
		if strings.HasPrefix(line, "›") {
			return line == "› Ask Codex to do anything"
		}
	}
	return false
}

// Use an inline table: Codex's CLI parser handles quoted dotted project keys differently.
func codexTrustOverride(cwd string) string {
	key, _ := json.Marshal(cwd)
	return "projects={" + string(key) + "={trust_level=\"trusted\"}}"
}
