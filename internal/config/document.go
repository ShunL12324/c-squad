package config

import "github.com/pelletier/go-toml/v2"

const documentHeader = `# C-Squad 配置（TOML）
# 用户配置：$XDG_CONFIG_HOME/csquad/config.toml，默认 ~/.config/csquad/config.toml。
# CSQUAD_CONFIG 可指定其他文件；项目 .csquad.toml 按字段覆盖用户配置。
# 未填写的字段沿用默认值。命令行参数优先于对应配置字段。
# 配置在团队启动时保存快照；修改文件不会改变已经运行的团队。
# 无需定义角色模板或账号列表；原生 MCP 和登录仍由 Claude Code / Codex 管理。
# TOML 中 [env] 后的键属于环境变量表；请将其他顶层配置放在 [env] 之前。

`

// Document encodes configuration values with field guidance for editing by hand.
// It is shared by first-run file creation and the config command; existing files
// are never rewritten just to update comments.
func Document(c Config) ([]byte, error) {
	if c.Env == nil {
		c.Env = map[string]string{}
	}
	b, err := toml.Marshal(c)
	if err != nil {
		return nil, err
	}
	return append([]byte(documentHeader), b...), nil
}
