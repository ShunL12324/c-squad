package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// Template is a legacy decode target only. Configurations that still carry a
// [templates] table are migrated to profiles at load; the type participates in
// no runtime decision. Prompt is dropped during migration because responsibilities
// belong to member add --instructions, not to a launch definition.
type Template struct {
	Env    map[string]string `json:"env,omitempty" toml:"env,omitempty" comment:"Legacy template environment overrides; migrated to a profile of the same name."`
	Engine Engine            `json:"engine,omitempty" toml:"engine,omitempty" comment:"Legacy template engine; migrated to a profile of the same name."`
	Model  string            `json:"model,omitempty" toml:"model,omitempty" comment:"Legacy template model; migrated to a profile of the same name."`
	Prompt string            `json:"prompt,omitempty" toml:"prompt,omitempty" comment:"Legacy template responsibilities. Discarded on migration; supply responsibilities with member add --instructions."`
}

// Config holds launch profiles and team-wide limits. Everything about how a
// member starts lives in a profile; the tables listed as legacy below are decoded
// only so an older file still loads, and are migrated into profiles at load.
// A startup snapshot is stored with the team so later config edits do not change it.
type Config struct {
	Profiles       map[string]Profile  `json:"profiles,omitempty" toml:"profiles,omitempty" comment:"Reusable launch definitions selected by name with member add --profile or start --profile.\nEach [profiles.NAME] table sets engine, model, env and an optional command, and is the only place launch settings are configured.\nNames use letters, digits, _ and - only.\nProfiles never carry responsibilities: a member's identity always comes from member add --instructions,\nand a profile name is never matched against a member."`
	DefaultProfile string              `json:"default_profile,omitempty" toml:"default_profile,omitempty" comment:"Profile used for members when member add omits --profile. Empty uses the built-in default: codex."`
	MasterProfile  string              `json:"master_profile,omitempty" toml:"master_profile,omitempty" comment:"Profile used for Master when start omits --profile. Empty uses the built-in default: claude with model opus[1m]."`
	Engine         Engine              `json:"engine,omitempty" toml:"engine,omitempty" comment:"Legacy field, migrated to a profile at load and then cleared. Select the member engine with default_profile."`
	Model          string              `json:"model,omitempty" toml:"model,omitempty" comment:"Legacy field, migrated to a profile at load and then cleared. Select the member model with default_profile."`
	MasterEngine   Engine              `json:"master_engine,omitempty" toml:"master_engine,omitempty" comment:"Legacy field, migrated to a profile at load and then cleared. Select the Master engine with master_profile."`
	MasterModel    string              `json:"master_model,omitempty" toml:"master_model,omitempty" comment:"Legacy field, migrated to a profile at load and then cleared. Select the Master model with master_profile."`
	Env            map[string]string   `json:"env,omitempty" toml:"env,omitempty" comment:"Legacy table, merged into every profile at load and then cleared. Set environment overrides in [profiles.NAME.env]."`
	StartupEnv     map[string]string   `json:"startup_env,omitempty" toml:"startup_env,omitempty" comment:"Legacy table, merged into every profile at load and then cleared. Set environment overrides in [profiles.NAME.env]."`
	EngineCommands map[Engine]Command  `json:"engine_commands,omitempty" toml:"engine_commands,omitempty" comment:"Legacy table, merged into the profiles using that engine at load and then cleared. Set a launcher in [profiles.NAME.command]."`
	Templates      map[string]Template `json:"templates,omitempty" toml:"templates,omitempty" comment:"Legacy role templates, migrated to profiles at load and then cleared. Define responsibilities with member add --instructions."`
	Version        int                 `json:"version" toml:"version" comment:"Configuration schema version. Only 2 is supported; a version 1 file is migrated to it at load.\nThis is not the application version; do not change it."`
	Bypass         bool                `json:"bypass_permissions" toml:"bypass_permissions" comment:"Bypass native permission approvals: true (default) or false.\nWhen true, uses Claude --dangerously-skip-permissions or Codex --yolo.\nAlso confirms Claude's native workspace-trust dialog for the selected working directory; Claude saves its normal project trust record. When false, native approvals remain enabled and members may wait for human approval.\nThis does not authenticate accounts, supply quota, or override organization policy."`
	MaxMembers     int                 `json:"max_members" toml:"max_members" comment:"Maximum team size, including Master. Default: 8; must be an integer of at least 1.\nRemoved members do not count. This does not limit conversation turns or task count."`
	PreviousKey    string              `json:"previous_member_key" toml:"previous_member_key" comment:"Key that switches to the previous member, in tmux key syntax. Default: M-Up (Alt/Option+Up).\nThe team binds it, so the agent CLI in the engine pane no longer receives it: tmux treats Alt and Meta as one M- namespace.\nSet it to an empty string to leave the key to the agent and navigate with Ctrl-b 0-9 or the sidebar instead."`
	NextKey        string              `json:"next_member_key" toml:"next_member_key" comment:"Key that switches to the next member, in tmux key syntax. Default: M-Down (Alt/Option+Down).\nSet it to an empty string to leave the key to the agent."`
}

// Version is the only supported schema version. A file still declaring 1 is
// migrated at load, including the tables that version allowed.
const Version = 2

// Defaults returns the built-in settings before user and project overlays.
// It deliberately defines no profiles: they would be merged back into every
// user file at load, so a profile the user deleted could never stay deleted.
// Load seeds them into a new file instead, and an unset profile pointer
// resolves to the built-in defaults in ResolveProfile.
func Defaults() Config {
	return Config{Version: Version, Bypass: true, MaxMembers: 8, PreviousKey: "M-Up", NextKey: "M-Down"}
}

// Keys are validated against tmux's own spelling so a mistyped binding fails at
// config load instead of silently never firing. An empty value disables it.
var (
	tmuxModifiers   = regexp.MustCompile(`^([CMS]-)+`)
	tmuxFunctionKey = regexp.MustCompile(`^[Ff]([1-9]|1[0-2])$`)
	tmuxNamedKeys   = map[string]bool{"up": true, "down": true, "left": true, "right": true,
		"bspace": true, "btab": true, "tab": true, "enter": true, "escape": true, "space": true,
		"home": true, "end": true, "ic": true, "insert": true, "dc": true, "delete": true,
		"npage": true, "pagedown": true, "pgdn": true, "ppage": true, "pageup": true, "pgup": true}
)

func validateKey(field, key string) error {
	name := tmuxModifiers.ReplaceAllString(key, "")
	switch {
	case key == "", len([]rune(name)) == 1, tmuxFunctionKey.MatchString(name), tmuxNamedKeys[strings.ToLower(name)]:
		return nil
	}
	return fmt.Errorf("%s: %q is not a tmux key name; use forms like M-Up, C-M-n or F5, or an empty string to leave the key unbound", field, key)
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

// encodeConfig is the counterpart of decodeConfig: the file extension selects
// the format, so every writer produces what the next load expects.
func encodeConfig(path string, c Config) ([]byte, error) {
	if strings.HasSuffix(path, ".json") {
		return json.MarshalIndent(c, "", "  ")
	}
	return Document(c)
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
		// Import the legacy user file once; retain it as a backup. It predates
		// profiles, so it is migrated before being written out: the new file has to
		// launch what the old one launched, which means the built-in profiles are
		// seeded only where the imported file left the choice to the defaults.
		legacy := strings.TrimSuffix(p, ".toml") + ".json"
		if strings.HasSuffix(p, ".toml") {
			if old, e := os.ReadFile(legacy); e == nil {
				if e = decodeConfig(legacy, old, &c); e != nil {
					return c, fmt.Errorf("%s: %w", legacy, e)
				}
				for _, warning := range MigrateLegacy(&c) {
					fmt.Fprintln(os.Stderr, "Configuration migration:", warning)
				}
			}
		}
		// The built-in launch profiles are written out as ordinary profiles, so a
		// new user opens the file and sees two definitions to edit rather than
		// having to guess what the defaults are. They carry no special meaning:
		// only the pointers make them the defaults, and either may be renamed,
		// edited, or deleted.
		seedProfiles(&c)
		if err = os.MkdirAll(filepath.Dir(p), 0700); err != nil {
			return c, err
		}
		if b, err = encodeConfig(p, c); err != nil {
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
	userPath, userFile := p, b
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
	// Migrate before validating: a legacy file has no profiles yet, and the
	// migrated result is what every later check and the whole run then use.
	for _, warning := range MigrateLegacy(&c) {
		fmt.Fprintln(os.Stderr, "Configuration migration:", warning)
	}
	if hasLegacyFields(userPath, userFile) {
		if err = writeBackMigration(userPath, userFile); err == nil {
			fmt.Fprintf(os.Stderr, "Configuration migration: %s was rewritten from its migrated values; the original is kept as %s\n", userPath, userPath+backupSuffix)
		} else {
			// Persisting is an optimisation; the migration already applies to this
			// run. A read-only or full filesystem must not stop a team from starting.
			fmt.Fprintf(os.Stderr, "Configuration migration: %s was migrated in memory but could not be updated (%v); the migration will run again next time\n", userPath, err)
		}
	}
	if c.Version != Version || c.MaxMembers < 1 {
		return c, fmt.Errorf("invalid config version or max_members")
	}
	if err = c.ValidateProfiles(); err != nil {
		return c, err
	}
	if err = c.requireProfileEngines(); err != nil {
		return c, err
	}
	for _, master := range []bool{true, false} {
		p, _, e := c.ResolveProfile("", master)
		if e != nil {
			return c, e
		}
		if err = p.Engine.Validate(); err != nil {
			return c, err
		}
	}
	if err = validateKey("previous_member_key", c.PreviousKey); err != nil {
		return c, err
	}
	if err = validateKey("next_member_key", c.NextKey); err != nil {
		return c, err
	}
	return c, nil
}
