package codex

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Options are a Codex task's thread options: the arguments after `--`,
// named as the codex command names them.
type Options struct {
	Model    string         // -m/--model
	Effort   string         // --effort: the turn's reasoning effort, kept by later turns
	Sandbox  string         // -s/--sandbox
	Approval string         // -a/--ask-for-approval
	Config   map[string]any // -c/--config KEY=VALUE, VALUE as JSON when it parses
	// configOrder keeps the -c keys in the order given.
	configOrder []string
}

// Defaults: like a Claude task in bypass mode.
const (
	DefaultSandbox  = "danger-full-access"
	DefaultApproval = "never"
)

var (
	sandboxes  = []string{"read-only", "workspace-write", "danger-full-access"}
	approvals  = []string{"untrusted", "on-request", "never"}
	efforts    = []string{"none", "minimal", "low", "medium", "high", "xhigh"}
	optionHelp = "-m/--model M, --effort E, -s/--sandbox MODE, -a/--ask-for-approval POLICY, -c/--config KEY=VALUE"
)

// ParseOptions reads thread options. It accepts `-m X`, `--model X` and
// `--model=X` (and `-mX` for the short names).
func ParseOptions(args []string) (Options, error) {
	o := Options{}
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, value, inline := a, "", false
		if strings.HasPrefix(a, "--") {
			name, value, inline = strings.Cut(a, "=")
		} else if strings.HasPrefix(a, "-") && len(a) > 2 {
			name, value, inline = a[:2], a[2:], true
		}
		long := map[string]string{"-m": "--model", "-s": "--sandbox", "-a": "--ask-for-approval", "-c": "--config"}
		if l, ok := long[name]; ok {
			name = l
		}
		switch name {
		case "--model", "--effort", "--sandbox", "--ask-for-approval", "--config":
		default:
			return o, fmt.Errorf("%s is not a Codex thread option (after -- a codex task takes %s)", a, optionHelp)
		}
		if !inline {
			if i+1 >= len(args) {
				return o, fmt.Errorf("%s needs a value", a)
			}
			i++
			value = args[i]
		}
		switch name {
		case "--model":
			if strings.TrimSpace(value) == "" {
				return o, fmt.Errorf("--model needs a value")
			}
			o.Model = value
		case "--effort":
			if !oneOf(value, efforts) {
				return o, fmt.Errorf("--effort %q: expected one of %s", value, strings.Join(efforts, ", "))
			}
			o.Effort = value
		case "--sandbox":
			if !oneOf(value, sandboxes) {
				return o, fmt.Errorf("--sandbox %q: expected one of %s", value, strings.Join(sandboxes, ", "))
			}
			o.Sandbox = value
		case "--ask-for-approval":
			if !oneOf(value, approvals) {
				return o, fmt.Errorf("--ask-for-approval %q: expected one of %s", value, strings.Join(approvals, ", "))
			}
			o.Approval = value
		case "--config":
			key, raw, ok := strings.Cut(value, "=")
			key = strings.TrimSpace(key)
			if !ok || key == "" {
				return o, fmt.Errorf("--config %q: expected KEY=VALUE", value)
			}
			var v any
			if json.Unmarshal([]byte(raw), &v) != nil {
				v = raw
			}
			if o.Config == nil {
				o.Config = map[string]any{}
			}
			if _, seen := o.Config[key]; !seen {
				o.configOrder = append(o.configOrder, key)
			}
			o.Config[key] = v
		}
	}
	return o, nil
}

func oneOf(v string, list []string) bool {
	for _, x := range list {
		if v == x {
			return true
		}
	}
	return false
}

// Args writes the options back in one canonical form, the one stored in the
// task's engine_args.
func (o Options) Args() []string {
	var out []string
	add := func(flag, v string) {
		if v != "" {
			out = append(out, flag, v)
		}
	}
	add("--model", o.Model)
	add("--effort", o.Effort)
	add("--sandbox", o.Sandbox)
	add("--ask-for-approval", o.Approval)
	for _, k := range o.configOrder {
		v := o.Config[k]
		s, ok := v.(string)
		if b, err := json.Marshal(v); !ok && err == nil {
			s = string(b)
		} else if ok && json.Valid([]byte(s)) {
			s, _ = jsonString(s) // a string that would read as JSON stays a string
		}
		out = append(out, "--config", k+"="+s)
	}
	if out == nil {
		out = []string{}
	}
	return out
}

func jsonString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}
