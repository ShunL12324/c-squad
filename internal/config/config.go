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
	Env    map[string]string `json:"env,omitempty" toml:"env,omitempty" comment:"旧模板专用环境覆盖，优先于全局 env；推荐改用 member add --env。"`
	Engine Engine            `json:"engine" toml:"engine" comment:"旧模板引擎，可选 claude 或 codex。"`
	Model  string            `json:"model,omitempty" toml:"model,omitempty" comment:"旧模板模型，空字符串沿用原生默认模型。"`
	Prompt string            `json:"prompt" toml:"prompt" comment:"旧模板职责说明，注入协作指令；推荐改用 member add --instructions。"`
}

// Config holds engine defaults, team limits, and environment overrides.
// A startup snapshot is stored with the team so later config edits do not change it.
type Config struct {
	Engine       Engine              `json:"engine" toml:"engine" comment:"普通成员默认引擎，可选 codex 或 claude；默认 codex。member add --engine 可覆盖。"`
	Model        string              `json:"model,omitempty" toml:"model" comment:"普通成员默认模型，默认空字符串：使用原生引擎配置。填写所选引擎支持的模型名；member add --model 可覆盖。"`
	MasterEngine Engine              `json:"master_engine" toml:"master_engine" comment:"Master 默认引擎，可选 codex 或 claude；默认 claude。start --engine 可覆盖。"`
	MasterModel  string              `json:"master_model,omitempty" toml:"master_model" comment:"Master 默认模型，默认 opus（Claude）。切换 master_engine 时请同步修改模型，或设为空字符串使用原生默认值。"`
	Env          map[string]string   `json:"env,omitempty" toml:"env" comment:"传给 Master 和所有成员的环境变量，默认不覆盖继承环境。值必须是字符串。\n优先级由低到高：继承环境 → env → start --env → member add --env。\n路径使用绝对路径，不展开 ~、$HOME 或命令替换。CODEX_HOME 可选择 Codex 配置目录。\nCLAUDE_CONFIG_DIR 可选择 Claude 配置目录；设为空字符串表示取消此变量。\n不要设置 CSQUAD_*、TMUX 或 TMUX_PANE，它们由程序管理。\n例如在下方 [env] 表内添加：CODEX_HOME = \"/home/yourname/.codex-alt\"。"`
	StartupEnv   map[string]string   `json:"startup_env,omitempty" toml:"startup_env,omitempty" comment:"兼容字段：团队启动时保存的环境覆盖，优先于 env。通常不要手动配置，请使用 start --env KEY=VALUE。"`
	Version      int                 `json:"version" toml:"version" comment:"配置格式版本，目前仅支持 1；不是程序版本，请勿修改。"`
	Bypass       bool                `json:"bypass_permissions" toml:"bypass_permissions" comment:"是否跳过原生权限审批：true（默认）或 false。\ntrue 使用 Claude --dangerously-skip-permissions / Codex --yolo；false 保留原生审批，成员可能等待人工确认。\n此设置不会完成登录，也不能绕过账户额度或组织策略。"`
	MaxMembers   int                 `json:"max_members" toml:"max_members" comment:"团队成员上限，包含 Master；默认 8，必须为大于等于 1 的整数。\n已移除成员不占名额；不是对话轮次或任务次数限制。"`
	Templates    map[string]Template `json:"templates,omitempty" toml:"templates,omitempty" comment:"旧版本角色模板兼容字段；新配置不需要它。成员职责请用 member add --role 和 --instructions 定义。"`
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
