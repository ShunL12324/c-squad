// Package config loads the user configuration from config.toml.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"

	"github.com/ShunL12324/c-squad/internal/paths"
)

// DefaultSlots is the number of concurrent background sessions when the
// configuration does not set one.
const DefaultSlots = 6

// MaxSlots bounds slots so a typo cannot start dozens of sessions at once.
const MaxSlots = 32

// Config holds the settings the dispatcher applies to queued sessions.
type Config struct {
	// Slots is how many launched sessions may be working or waiting for input
	// at the same time, across every project.
	Slots int `toml:"slots"`
	// Model is the default --model for queued sessions; a task can override it.
	Model string `toml:"model"`
	// PermissionMode is passed to queued sessions as --permission-mode.
	PermissionMode string `toml:"permission_mode"`
}

// Path returns the configuration file location.
func Path() (string, error) {
	dir, err := paths.Config()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// Load reads the configuration file. A missing file yields the defaults, and
// unknown keys, including those of the previous team format, are ignored.
func Load() (Config, error) {
	cfg := Config{Slots: DefaultSlots}
	path, err := Path()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := toml.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("parse %s: %w", path, err)
	}
	if cfg.Slots == 0 {
		cfg.Slots = DefaultSlots
	}
	if cfg.Slots < 1 || cfg.Slots > MaxSlots {
		return cfg, fmt.Errorf("%s: slots must be between 1 and %d, got %d", path, MaxSlots, cfg.Slots)
	}
	return cfg, nil
}
