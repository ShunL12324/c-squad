package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// Template holds engine defaults and supports legacy named role configurations.
type Template struct {
	Env    map[string]string `json:"env,omitempty" toml:"env,omitempty" comment:"Legacy template environment overrides; take precedence over global env. Prefer member add --env."`
	Engine Engine            `json:"engine" toml:"engine" comment:"Legacy template engine: claude or codex."`
	Model  string            `json:"model,omitempty" toml:"model,omitempty" comment:"Legacy template model. An empty string uses the native engine default."`
	Prompt string            `json:"prompt" toml:"prompt" comment:"Legacy template responsibilities, injected into coordination instructions. Prefer member add --instructions."`
}

// Config holds engine defaults, team limits, and environment overrides.
// A startup snapshot is stored with the team so later config edits do not change it.
type Config struct {
	Engine       Engine              `json:"engine" toml:"engine" comment:"Default worker engine: codex (default) or claude. Override with member add --engine."`
	Model        string              `json:"model,omitempty" toml:"model" comment:"Default worker model. Empty by default to use native engine configuration. Use a model supported by the selected engine; override with member add --model."`
	MasterEngine Engine              `json:"master_engine" toml:"master_engine" comment:"Default Master engine: claude (default) or codex. Override with start --engine."`
	MasterModel  string              `json:"master_model,omitempty" toml:"master_model" comment:"Default Master model: opus (Claude). When changing master_engine, also change this model or set it to an empty string to use the native default."`
	Env          map[string]string   `json:"env,omitempty" toml:"env" comment:"Environment overrides for Master and all members. Empty by default; values must be strings.\nPrecedence, lowest to highest: inherited environment -> env -> start --env -> member add --env.\nUse absolute paths. Paths do not expand ~, $HOME, or command substitutions. CODEX_HOME selects the Codex configuration directory.\nCLAUDE_CONFIG_DIR selects the Claude configuration directory. An empty value unsets the variable.\nCSQUAD_*, TMUX, and TMUX_PANE are managed by C Squad and cannot be overridden.\nExample entry in the [env] table below: CODEX_HOME = \"/home/yourname/.codex-alt\"."`
	StartupEnv   map[string]string   `json:"startup_env,omitempty" toml:"startup_env,omitempty" comment:"Compatibility field for saved startup environment overrides; takes precedence over env. Normally set these through start --env KEY=VALUE rather than editing this field."`
	Version      int                 `json:"version" toml:"version" comment:"Configuration schema version. Only 1 is supported. This is not the application version; do not change it."`
	Bypass       bool                `json:"bypass_permissions" toml:"bypass_permissions" comment:"Bypass native permission approvals: true (default) or false.\nWhen true, uses Claude --dangerously-skip-permissions or Codex --yolo.\nAlso confirms Claude's native workspace-trust dialog for the selected working directory; Claude saves its normal project trust record. When false, native approvals remain enabled and members may wait for human approval.\nThis does not authenticate accounts, supply quota, or override organization policy."`
	MaxMembers   int                 `json:"max_members" toml:"max_members" comment:"Maximum team size, including Master. Default: 8; must be an integer of at least 1.\nRemoved members do not count. This does not limit conversation turns or task count."`
	Templates    map[string]Template `json:"templates,omitempty" toml:"templates,omitempty" comment:"Legacy role template compatibility field; new configurations do not need it. Define member responsibilities with member add --role and --instructions."`
}

// Defaults returns the built-in settings before user and project overlays.
func Defaults() Config {
	return Config{Version: 1, Bypass: true, MaxMembers: 8, Engine: Codex, MasterEngine: Claude, MasterModel: "opus"}
}

// EngineDefaults resolves launch defaults for master or worker.
// Legacy templates provide a fallback only when the corresponding engine is unset.
func EngineDefaults(c Config, master bool) Template {
	if master {
		if c.MasterEngine != "" {
			return Template{Engine: c.MasterEngine, Model: c.MasterModel}
		}
		if t, ok := c.Templates["master"]; ok {
			return t
		}
		return Template{Engine: Claude, Model: "opus"}
	}
	if c.Engine != "" {
		return Template{Engine: c.Engine, Model: c.Model}
	}
	if t, ok := c.Templates["developer"]; ok {
		return t
	}
	return Template{Engine: Codex}
}

// Path resolves the user configuration path, honoring CSQUAD_CONFIG before XDG_CONFIG_HOME.
func Path() string {
	if p := os.Getenv("CSQUAD_CONFIG"); p != "" {
		return p
	}
	d := os.Getenv("XDG_CONFIG_HOME")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, ".config")
	}
	return filepath.Join(d, "csquad", "config.toml")
}
func decodeConfig(path string, b []byte, c *Config) error {
	// Decode into the schema first so typoed keys cannot disappear in the map overlay.
	// The second decode retains field presence for partial updates and legacy migration.
	var schema Config
	if strings.HasSuffix(path, ".json") {
		decoder := json.NewDecoder(bytes.NewReader(b))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&schema); err != nil {
			return err
		}
	} else {
		if err := toml.NewDecoder(bytes.NewReader(b)).DisallowUnknownFields().Decode(&schema); err != nil {
			return err
		}
	}
	var patch map[string]any
	var err error
	if strings.HasSuffix(path, ".json") {
		err = json.Unmarshal(b, &patch)
	} else {
		err = toml.Unmarshal(b, &patch)
	}
	if err != nil {
		return err
	}
	original, _ := json.Marshal(c)
	var base map[string]any
	if err = json.Unmarshal(original, &base); err != nil {
		return err
	}
	// Import old default engine choices, while dynamic roles no longer inherit
	// the old developer identity. Explicit new fields always win.
	if templates, ok := patch["templates"].(map[string]any); ok {
		for role, keys := range map[string][]string{"master": {"master_engine", "master_model"}, "developer": {"engine", "model"}} {
			if t, ok := templates[role].(map[string]any); ok {
				for i, field := range []string{"engine", "model"} {
					if _, explicit := patch[keys[i]]; !explicit {
						if value, exists := t[field]; exists {
							patch[keys[i]] = value
						}
					}
				}
			}
		}
	}
	mergeConfigMap(base, patch)
	merged, err := json.Marshal(base)
	if err != nil {
		return err
	}
	return json.Unmarshal(merged, c)
}
func mergeConfigMap(base, patch map[string]any) {
	for k, v := range patch {
		child, ok := v.(map[string]any)
		old, oldOK := base[k].(map[string]any)
		if ok && oldOK {
			mergeConfigMap(old, child)
		} else {
			base[k] = v
		}
	}
}

// Load applies built-in, user, and project settings in that order.
// A missing user file is created with private permissions; a legacy JSON file is
// imported without deleting it. Project overlays never modify the user file.
func Load(root string) (Config, error) {
	c := Defaults()
	p := Path()
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		// Import the legacy user file once; retain it as a backup.
		legacy := strings.TrimSuffix(p, ".toml") + ".json"
		if strings.HasSuffix(p, ".toml") {
			if old, e := os.ReadFile(legacy); e == nil {
				if e = decodeConfig(legacy, old, &c); e != nil {
					return c, fmt.Errorf("%s: %w", legacy, e)
				}
			}
		}
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return c, err
		}
		if strings.HasSuffix(p, ".json") {
			b, err = json.MarshalIndent(c, "", "  ")
		} else {
			b, err = Document(c)
		}
		if err != nil {
			return c, err
		}
		f, e := os.OpenFile(p, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e == nil {
			_, err = f.Write(b)
			closeErr := f.Close()
			if err == nil {
				err = closeErr
			}
		} else if os.IsExist(e) {
			b, err = os.ReadFile(p)
		} else {
			err = e
		}
	}
	if err != nil {
		return c, err
	}
	if err = decodeConfig(p, b, &c); err != nil {
		return c, fmt.Errorf("%s: %w", p, err)
	}
	if root != "" {
		p = filepath.Join(root, ".csquad.toml")
		b, err = os.ReadFile(p)
		if os.IsNotExist(err) {
			p = filepath.Join(root, ".csquad.json")
			b, err = os.ReadFile(p)
		}
		if err == nil {
			if err = decodeConfig(p, b, &c); err != nil {
				return c, fmt.Errorf("%s: %w", p, err)
			}
		} else if !os.IsNotExist(err) {
			return c, err
		}
	}
	if c.Version != 1 || c.MaxMembers < 1 || EngineDefaults(c, true).Engine == "" {
		return c, fmt.Errorf("invalid config version, max_members or master engine")
	}
	for _, engine := range []Engine{EngineDefaults(c, true).Engine, EngineDefaults(c, false).Engine} {
		if err = engine.Validate(); err != nil {
			return c, err
		}
	}
	if err = agentenv.Validate(c.Env); err != nil {
		return c, err
	}
	if err = agentenv.Validate(c.StartupEnv); err != nil {
		return c, err
	}
	for _, t := range c.Templates {
		if t.Engine != "" {
			if err = t.Engine.Validate(); err != nil {
				return c, err
			}
		}
		if err = agentenv.Validate(t.Env); err != nil {
			return c, err
		}
	}
	return c, nil
}
