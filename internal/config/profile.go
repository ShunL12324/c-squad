package config

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
)

// Profile is a reusable launch definition: which engine and model to start, the
// environment that selects the account, and an optional command override. It
// deliberately carries no prompt, instructions, or responsibilities. Identity is
// supplied per member with member add --instructions, so a profile name never
// implies what a member is for.
type Profile struct {
	Env     map[string]string `json:"env,omitempty" toml:"env,omitempty" comment:"Environment overrides for members launched with this profile, and the only place they are configured.\nPrecedence, lowest to highest: inherited environment -> this table.\nUse absolute paths. Paths do not expand ~, $HOME, or command substitutions. CODEX_HOME selects the Codex configuration directory.\nCLAUDE_CONFIG_DIR selects the Claude configuration directory. An empty value unsets the variable.\nCSQUAD_*, TMUX, and TMUX_PANE are managed by C Squad and cannot be overridden."`
	Command *Command          `json:"command,omitempty" toml:"command,omitempty" comment:"Optional executable and literal prefix arguments used to launch this profile's engine, including probes and helper subcommands.\nOmit the table to run the engine by name from PATH.\nUse an absolute path or a PATH command name; shell is bash or zsh for a persistent alias; no shell parsing or expansion."`
	Engine  Engine            `json:"engine,omitempty" toml:"engine,omitempty" comment:"Engine started by this profile: claude or codex. Required. Select the profile with member add --profile or start --profile."`
	Model   string            `json:"model,omitempty" toml:"model,omitempty" comment:"Model started by this profile. An empty string uses the native engine default.\nUse a model supported by the selected engine."`
}

// Built-in launch defaults. A new configuration is created with these written
// out under the names below, so the defaults are visible and editable. They stay
// in code as well, as the fallback for a file whose pointers are empty or whose
// profile a member was added with has since been deleted; a team must still start.
var (
	builtinWorkerProfile = Profile{Engine: Codex}
	builtinMasterProfile = Profile{Engine: Claude, Model: "opus[1m]"}
)

// Names used for the written-out built-in profiles. They are ordinary names:
// only default_profile and master_profile make them the defaults.
const (
	defaultProfileName = "codex"
	masterProfileName  = "claude-opus"
)

// Profile names appear as TOML keys, which hold non-ASCII characters only when
// quoted. Quoting round-trips correctly here, so this is a usability limit
// rather than a format one: a name is typed after --profile and printed in
// diagnostics, and one needing quotes reads as a quoting mistake wherever it
// appears.
var profileName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ResolveProfile selects a launch profile by explicit name, falling back to the
// configured pointer for the requested role and then to the built-in defaults.
// An unknown name lists what is available instead of failing silently.
//
// The resolved name is returned alongside the profile and is empty only when the
// built-in defaults applied. Callers record it on the member so a command
// override still applies when the profile came from a pointer rather than a flag.
func (c Config) ResolveProfile(name string, master bool) (Profile, string, error) {
	explicit := name != ""
	if !explicit {
		if name = c.DefaultProfile; master {
			name = c.MasterProfile
		}
	}
	if name == "" {
		if master {
			return builtinMasterProfile, "", nil
		}
		return builtinWorkerProfile, "", nil
	}
	p, ok := c.Profiles[name]
	if !ok {
		field := "default_profile"
		if explicit {
			field = "--profile"
		} else if master {
			field = "master_profile"
		}
		return Profile{}, "", fmt.Errorf("%s: unknown profile %q; %s", field, name, c.profileChoices())
	}
	return p, name, nil
}

// profileChoices renders the configured names in a stable order so the same
// mistake always produces the same message.
func (c Config) profileChoices() string {
	if len(c.Profiles) == 0 {
		return "no profiles are defined; add a [profiles.NAME] table"
	}
	names := make([]string, 0, len(c.Profiles))
	for name := range c.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return "available profiles: " + strings.Join(names, ", ")
}

// ProfileCommand resolves the launch command for a member. Unlike the engine,
// model and environment, which are materialised when the member is added, the
// command is read from the profile at launch, so an edited launcher reaches the
// next restart. A profile deleted after the member was added falls back to the
// built-in default, which runs the engine by name, and warns instead of failing:
// editing a configuration must not stop an existing team from starting.
func (c Config) ProfileCommand(name string, engine Engine) (Command, string) {
	if name == "" {
		return EngineCommand(engine), ""
	}
	p, ok := c.Profiles[name]
	if !ok {
		return EngineCommand(engine), fmt.Sprintf("profile %q is no longer defined; launching %s from the built-in default profile", name, engine)
	}
	return p.LaunchCommand(engine), ""
}

// LaunchCommand fills in the engine name for a profile that defines no command
// or no executable, so callers always receive something they can run.
func (p Profile) LaunchCommand(engine Engine) Command {
	if p.Command == nil {
		return EngineCommand(engine)
	}
	command := *p.Command
	if command.Executable == "" {
		command.Executable = string(engine)
	}
	return command
}

// EngineCommand runs an engine by its own name, which is what a profile without
// a command table launches.
func EngineCommand(engine Engine) Command {
	return Command{Executable: string(engine)}
}

// LaunchFor reports how an engine would be started, preferring the profiles the
// pointers select and then any profile using that engine. Checks and diagnostics
// probe what a team would really launch rather than the bare engine name.
func (c Config) LaunchFor(engine Engine) (Command, map[string]string) {
	if name := c.LaunchProfileFor(engine, true); name != "" {
		p := c.Profiles[name]
		return p.LaunchCommand(engine), p.Env
	}
	return EngineCommand(engine), nil
}

// LaunchProfileFor names the profile that starts an engine, preferring the
// pointer for the requested role and then any profile using it. Migration needs
// the name rather than the profile: a member saved before profiles existed
// carries none, and the profile its legacy launch settings became is the one it
// has to be pointed at. An empty result means no profile launches that engine.
func (c Config) LaunchProfileFor(engine Engine, master bool) string {
	pointers := []string{c.DefaultProfile, c.MasterProfile}
	if master {
		pointers = []string{c.MasterProfile, c.DefaultProfile}
	}
	for _, name := range append(pointers, sortedProfileNames(c.Profiles)...) {
		if p, ok := c.Profiles[name]; ok && p.Engine == engine {
			return name
		}
	}
	return ""
}

// ValidateProfiles checks every profile so a bad one fails at load instead of at
// member add or launch, when a team is already running.
func (c Config) ValidateProfiles() error {
	for _, name := range sortedProfileNames(c.Profiles) {
		p := c.Profiles[name]
		if !profileName.MatchString(name) {
			return fmt.Errorf("profiles.%s: profile names use letters, digits, _ and - only", name)
		}
		if p.Engine != "" {
			if err := p.Engine.Validate(); err != nil {
				return fmt.Errorf("profiles.%s: %w", name, err)
			}
		}
		if err := agentenv.Validate(p.Env); err != nil {
			return fmt.Errorf("profiles.%s.env: %w", name, err)
		}
		if p.Command != nil {
			if err := validateCommand("profiles."+name+".command", p.Engine, *p.Command); err != nil {
				return err
			}
		}
	}
	for _, pointer := range []struct {
		field string
		name  string
	}{{"default_profile", c.DefaultProfile}, {"master_profile", c.MasterProfile}} {
		if pointer.name == "" {
			continue
		}
		if _, ok := c.Profiles[pointer.name]; !ok {
			return fmt.Errorf("%s: unknown profile %q; %s", pointer.field, pointer.name, c.profileChoices())
		}
	}
	return nil
}

// requireProfileEngines rejects a profile that names no engine when the file
// loads, instead of at member add or start with an unsupported engine "". Legacy
// templates without one are given their default engine by migration first. It is
// kept out of ValidateProfiles, which also checks a running team's saved
// snapshot: a snapshot taken before this check may hold such a profile unused,
// and it must not stop that team's members from launching.
func (c Config) requireProfileEngines() error {
	for _, name := range sortedProfileNames(c.Profiles) {
		if c.Profiles[name].Engine == "" {
			return fmt.Errorf("profiles.%s: engine is required; set engine = %q or %q", name, Claude, Codex)
		}
	}
	return nil
}

func sortedProfileNames(profiles map[string]Profile) []string {
	names := make([]string, 0, len(profiles))
	for name := range profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
