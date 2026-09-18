package config

import "fmt"

// Engine selects a supported native executable. Model names remain engine-defined
// strings; engine values retain their JSON/TOML representation across upgrades.
type Engine string

// Supported Engine values.
const (
	Claude Engine = "claude"
	Codex  Engine = "codex"
)

// Validate rejects unsupported engines at configuration and CLI boundaries.
// String encoding is retained for compatibility with existing TOML and ledgers.
func (e Engine) Validate() error {
	switch e {
	case Claude, Codex:
		return nil
	default:
		return fmt.Errorf("unsupported engine %q; choose claude or codex", e)
	}
}
