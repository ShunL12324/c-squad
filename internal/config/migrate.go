package config

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// backupSuffix marks the pre-migration copy kept beside the user configuration,
// matching how earlier one-time migrations in this project preserved originals.
const backupSuffix = ".before-profiles"

var unsafeProfileChar = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

// MigrateLegacy folds every legacy launch setting into profiles and returns one
// warning per change. It is pure, idempotent, and safe to run on any decoded
// configuration: one that already uses profiles produces no warnings.
//
// Four legacy shapes are handled. A [templates] table becomes a profile of the
// same name, dropping its prompt because responsibilities are supplied per member.
// Top-level engine/model and master_engine/master_model become profiles that the
// pointer fields select. [env] and [startup_env] applied to everyone, so they are
// merged into every profile, and an [engine_commands.ENGINE] entry is merged into
// the profiles launching that engine. The result launches what the file launched
// before, from the one place launch settings now live.
func MigrateLegacy(c *Config) []string {
	warnings, migrated := migrateTemplates(c)
	// The legacy templates named developer and master were the defaults for their
	// roles whenever the matching top-level field was unset. That fallback is
	// reproduced here, in a one-time migration, so upgrading does not silently
	// change which engine a team starts. Only a profile this migration just
	// created from a template qualifies: a profile the user wrote is an ordinary
	// profile, so naming one master never makes it Master's. Nothing at runtime
	// matches a profile name against a member again either.
	for _, target := range []struct {
		field    string
		pointer  *string
		engine   Engine
		model    string
		written  bool
		inherits string
		master   bool
	}{
		{"default_profile", &c.DefaultProfile, c.Engine, c.Model, false, "developer", false},
		// An explicit master_model = "" chose the native default model over the
		// built-in one, so it migrates like any other written value.
		{"master_profile", &c.MasterProfile, c.MasterEngine, c.MasterModel, c.masterModelWritten, "master", true},
	} {
		if *target.pointer != "" {
			if target.engine != "" || target.model != "" {
				// Typically a project overlay predating profiles, merged over a user
				// file that already selects one. The pointer wins; say so rather than
				// drop the setting silently.
				warnings = append(warnings, fmt.Sprintf("legacy engine settings ignored because %s selects profiles.%s; set them in that profile or select another with --profile", target.field, *target.pointer))
			}
			continue
		}
		switch {
		case target.engine != "" || target.model != "" || target.written:
			// The role's template supplied whatever the top-level fields left
			// out, its environment included, so the profile starts from it and
			// only the fields the file wrote replace the template's values.
			p := Profile{}
			if migrated[target.inherits] {
				p = c.Profiles[target.inherits]
				if p.Env != nil {
					// Copy, so the template's own profile is never changed.
					p.Env = agentenv.Merge(p.Env)
				}
			}
			if target.engine != "" {
				p.Engine = target.engine
			}
			if target.model != "" || target.written {
				p.Model = target.model
			}
			if p.Engine == "" {
				// Before profiles the engine always had a default, so a file that
				// set only a model still launched the built-in engine.
				if p.Engine = builtinWorkerProfile.Engine; target.master {
					p.Engine = builtinMasterProfile.Engine
				}
			}
			if migrated[target.inherits] && reflect.DeepEqual(p, c.Profiles[target.inherits]) {
				*target.pointer = target.inherits
				warnings = append(warnings, fmt.Sprintf("profiles.%s now selected by %s, matching the previous templates.%s default", target.inherits, target.field, target.inherits))
				continue
			}
			name := adoptProfile(c, p, target.master)
			*target.pointer = name
			warnings = append(warnings, fmt.Sprintf("legacy engine settings migrated to profiles.%s, selected by %s", name, target.field))
			if migrated[target.inherits] && len(p.Env) > 0 {
				warnings = append(warnings, fmt.Sprintf("profiles.%s keeps the env of templates.%s, which the legacy settings only partly overrode", name, target.inherits))
			}
		case migrated[target.inherits]:
			// Point at the migrated template instead of copying it, so its env and
			// command survive the change of ownership.
			*target.pointer = target.inherits
			warnings = append(warnings, fmt.Sprintf("profiles.%s now selected by %s, matching the previous templates.%s default", target.inherits, target.field, target.inherits))
		}
	}
	c.Engine, c.Model, c.MasterEngine, c.MasterModel, c.masterModelWritten = "", "", "", "", false
	warnings = append(warnings, migrateSharedTables(c)...)
	if c.Version != 0 && c.Version < Version {
		// Version 0 means the file declared no version at all; Load rejects that
		// afterwards rather than silently adopting a schema the file never claimed.
		c.Version = Version
	}
	return warnings
}

// migrateSharedTables folds the tables that used to apply to several members at
// once into the profiles they reached, in the order the old launch path merged
// them: [env] was the lowest layer, a template's own env sat above it, and
// [startup_env] above both. Preserving that order matters where the same key
// appears twice, because the merged profile has to launch what the file launched.
func migrateSharedTables(c *Config) []string {
	if len(c.Env) == 0 && len(c.StartupEnv) == 0 && len(c.EngineCommands) == 0 {
		return nil
	}
	// These tables reached whatever the file launched, including the built-in
	// defaults, which are code constants with no profile to merge into. Writing
	// the defaults out first gives them one; otherwise a file that configured
	// only [env] would lose the variable selecting its account.
	warnings := materialiseDefaults(c)
	for _, table := range []struct {
		field  string
		values map[string]string
	}{{"env", c.Env}, {"startup_env", c.StartupEnv}} {
		if len(table.values) > 0 {
			warnings = append(warnings, fmt.Sprintf("%s merged into every profile's env; set environment overrides in [profiles.NAME.env]", table.field))
		}
	}
	for _, engine := range sortedEngines(c.EngineCommands) {
		command := c.EngineCommands[engine]
		used := false
		for _, name := range sortedProfileNames(c.Profiles) {
			p := c.Profiles[name]
			if p.Engine != engine || p.Command != nil {
				continue
			}
			p.Command = &command
			c.Profiles[name] = p
			used = true
		}
		if used {
			warnings = append(warnings, fmt.Sprintf("engine_commands.%s merged into the profiles launching %s; set a launcher in [profiles.NAME.command]", engine, engine))
			continue
		}
		if engine.Validate() != nil {
			// An entry for something that is not an engine has nothing to launch and
			// would fail validation as a profile, so it is reported and dropped.
			warnings = append(warnings, fmt.Sprintf("engine_commands.%s dropped because %s is not an engine; set a launcher in [profiles.NAME.command]", engine, engine))
			continue
		}
		// No profile launches this engine, but a saved team can still hold members
		// that do: the engine used to be chosen per member. Giving the command a
		// profile of its own keeps those members on their configured launcher
		// instead of dropping them onto the bare engine name.
		name := writeProfile(c, string(engine), Profile{Engine: engine, Command: &command})
		warnings = append(warnings, fmt.Sprintf("engine_commands.%s preserved as profiles.%s, because no other profile launches %s; set a launcher in [profiles.NAME.command]", engine, name, engine))
	}
	if len(c.Env) > 0 || len(c.StartupEnv) > 0 {
		for _, name := range sortedProfileNames(c.Profiles) {
			p := c.Profiles[name]
			p.Env = agentenv.Merge(c.Env, p.Env, c.StartupEnv)
			c.Profiles[name] = p
		}
	}
	c.Env, c.StartupEnv, c.EngineCommands = nil, nil, nil
	return warnings
}

// seedProfiles writes the built-in profiles into the configuration and points
// the pointers that are still empty at them, so the defaults are visible in the
// file instead of only in code. An existing profile that already launches the
// same thing is reused rather than duplicated, and nothing is overwritten. It
// returns the pointer fields it filled, in order.
func seedProfiles(c *Config) []string {
	var filled []string
	for _, target := range []struct {
		field   string
		pointer *string
		name    string
		profile Profile
	}{
		{"default_profile", &c.DefaultProfile, defaultProfileName, builtinWorkerProfile},
		{"master_profile", &c.MasterProfile, masterProfileName, builtinMasterProfile},
	} {
		if *target.pointer != "" {
			continue
		}
		*target.pointer = writeProfile(c, target.name, target.profile)
		filled = append(filled, target.field)
	}
	return filled
}

// materialiseDefaults seeds the built-in profiles during migration, where the
// reason matters: a shared legacy table reached whatever the file launched,
// including the built-in defaults, which had no profile to merge into.
func materialiseDefaults(c *Config) []string {
	var warnings []string
	for _, field := range seedProfiles(c) {
		name := c.DefaultProfile
		if field == "master_profile" {
			name = c.MasterProfile
		}
		warnings = append(warnings, fmt.Sprintf("built-in defaults written out as profiles.%s, selected by %s, so the legacy tables have a profile to merge into", name, field))
	}
	return warnings
}

// writeProfile returns the name of an existing profile that already launches
// what the built-in one would, or stores the given one under a free name derived
// from preferred. A profile carrying its own env or command is never adopted as a
// default: it was written for a particular use, not as everyone's fallback.
func writeProfile(c *Config, preferred string, p Profile) string {
	for _, name := range sortedProfileNames(c.Profiles) {
		if sameLaunch(c.Profiles[name], p) {
			return name
		}
	}
	name := preferred
	for i := 2; ; i++ {
		if _, taken := c.Profiles[name]; !taken {
			break
		}
		name = preferred + "-" + strconv.Itoa(i)
	}
	setProfile(c, name, p)
	return name
}

func sortedEngines(commands map[Engine]Command) []Engine {
	engines := make([]Engine, 0, len(commands))
	for engine := range commands {
		engines = append(engines, engine)
	}
	sort.Slice(engines, func(i, j int) bool { return engines[i] < engines[j] })
	return engines
}

// migrateTemplates converts each legacy template into a profile of the same name
// and clears the table. An explicitly defined profile always wins. The names it
// created are returned so only those inherit a legacy role default.
func migrateTemplates(c *Config) ([]string, map[string]bool) {
	var warnings []string
	migrated := map[string]bool{}
	for _, name := range sortedTemplateNames(c.Templates) {
		t := c.Templates[name]
		if _, defined := c.Profiles[name]; defined {
			warnings = append(warnings, fmt.Sprintf("templates.%s ignored because profiles.%s is defined explicitly", name, name))
			continue
		}
		// Only a template that selected an engine or model stood in for a role
		// default, so the engine filled in below must not make one qualify.
		migrated[name] = t.Engine != "" || t.Model != ""
		engine := t.Engine
		if engine == "" {
			// A template without an engine launched the role's default engine, and
			// a profile has no such fallback: it must name one to load.
			engine = legacyDefaultEngine(c, name == "master")
			warnings = append(warnings, fmt.Sprintf("templates.%s set no engine; profiles.%s launches %s, the engine it defaulted to", name, name, engine))
		}
		setProfile(c, name, Profile{Env: t.Env, Engine: engine, Model: t.Model})
		warnings = append(warnings, fmt.Sprintf("templates.%s migrated to profiles.%s", name, name))
		if t.Prompt != "" {
			warnings = append(warnings, fmt.Sprintf("templates.%s.prompt discarded; supply responsibilities with member add --instructions", name))
		}
	}
	c.Templates = nil
	return warnings, migrated
}

// legacyDefaultEngine is the engine a role launched before profiles when nothing
// more specific chose one: the top-level setting, then the built-in default.
func legacyDefaultEngine(c *Config, master bool) Engine {
	if master {
		if c.MasterEngine != "" {
			return c.MasterEngine
		}
		return builtinMasterProfile.Engine
	}
	if c.Engine != "" {
		return c.Engine
	}
	return builtinWorkerProfile.Engine
}

// adoptProfile reuses an equivalent existing profile and otherwise stores the
// generated one under a free name. User-defined profiles are never overwritten.
func adoptProfile(c *Config, p Profile, master bool) string {
	for _, name := range sortedProfileNames(c.Profiles) {
		if sameLaunch(c.Profiles[name], p) {
			return name
		}
	}
	base := generatedName(p, master)
	name := base
	for i := 2; ; i++ {
		if _, taken := c.Profiles[name]; !taken {
			break
		}
		name = base + "-" + strconv.Itoa(i)
	}
	setProfile(c, name, p)
	return name
}

// generatedName derives a readable name from the engine and model the legacy
// fields selected, sanitised into a TOML bare key.
func generatedName(p Profile, master bool) string {
	base := string(p.Engine)
	if base == "" {
		if base = "default"; master {
			base = "master"
		}
	}
	if p.Model != "" {
		base += "-" + p.Model
	}
	base = unsafeProfileChar.ReplaceAllString(base, "-")
	if !profileName.MatchString(base) {
		return "migrated"
	}
	return base
}

// sameLaunch reports whether an existing profile already starts what the
// generated one would. A generated profile carries no command, and env only
// when it was derived from a legacy template, so a profile is equivalent only
// with the same env and no command: reusing one without the template's env
// would drop the account it selected.
func sameLaunch(existing, generated Profile) bool {
	return existing.Engine == generated.Engine && existing.Model == generated.Model &&
		maps.Equal(existing.Env, generated.Env) && existing.Command == nil && generated.Command == nil
}

func setProfile(c *Config, name string, p Profile) {
	if c.Profiles == nil {
		c.Profiles = map[string]Profile{}
	}
	c.Profiles[name] = p
}

func sortedTemplateNames(templates map[string]Template) []string {
	names := make([]string, 0, len(templates))
	for name := range templates {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// hasLegacyFields reports whether a decoded file carries anything to migrate,
// including an older schema version, which is what the rewritten file updates.
func hasLegacyFields(path string, b []byte) bool {
	probe := Config{}
	if decodeConfig(path, b, &probe) != nil {
		return false
	}
	return probe.Engine != "" || probe.Model != "" || probe.MasterEngine != "" ||
		probe.MasterModel != "" || probe.masterModelWritten || len(probe.Templates) > 0 || len(probe.Env) > 0 ||
		len(probe.StartupEnv) > 0 || len(probe.EngineCommands) > 0 ||
		(probe.Version != 0 && probe.Version < Version)
}

// writeBackMigration persists a migrated user configuration after copying the
// original aside. Only the user file is rewritten: values merged from a project
// overlay belong to that project, not to the user's file. Callers treat every
// error as advisory, because the in-memory migration already governs this run.
func writeBackMigration(path string, original []byte) error {
	backup := path + backupSuffix
	f, err := os.OpenFile(backup, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		// An existing backup is an earlier migration's copy of the original file.
		// Never replace it with content that has already been migrated, and do not
		// rewrite without a backup; report it so the caller does not claim success.
		if os.IsExist(err) {
			return fmt.Errorf("%s already exists from an earlier migration; move it aside to allow the rewrite", backup)
		}
		return err
	}
	_, err = f.Write(original)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	// Migrate the user file on its own so the rewritten file never absorbs values
	// that only a project overlay supplied.
	single := Defaults()
	if err = decodeConfig(path, original, &single); err != nil {
		return err
	}
	MigrateLegacy(&single)
	encoded, err := encodeConfig(path, single)
	if err != nil {
		return err
	}
	// Replace the file a symlinked configuration points at, so the link (often
	// into a dotfiles repository) stays in place and receives the migration.
	target := path
	if resolved, e := filepath.EvalSymlinks(path); e == nil {
		target = resolved
	}
	return writeFileAtomic(target, encoded)
}

// writeFileAtomic replaces a file through a same-directory temporary file so an
// interrupted write cannot leave a partial configuration behind.
func writeFileAtomic(path string, b []byte) error {
	temp, err := os.CreateTemp(filepath.Dir(path), ".csquad-config-*")
	if err != nil {
		return err
	}
	_, err = temp.Write(b)
	if chmodErr := temp.Chmod(0600); err == nil {
		err = chmodErr
	}
	if closeErr := temp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temp.Name())
	}
	return err
}
