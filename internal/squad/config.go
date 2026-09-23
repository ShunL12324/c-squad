package squad

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
)

func printConfig(c config.Config) error {
	b, err := config.Document(c)
	if err != nil {
		return err
	}
	fmt.Printf("# %s\n%s", config.Path(), b)
	return nil
}

func stateBase() string {
	if d := os.Getenv("CSQUAD_HOME"); d != "" {
		return d
	}
	d := os.Getenv("XDG_STATE_HOME")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, ".local", "state")
	}
	return filepath.Join(d, "csquad")
}

// migrateSnapshotConfig converts a team's startup snapshot to profiles and
// returns one warning per change. A team started before profiles existed keeps
// legacy engine fields in its snapshot, and effectiveConfig never falls back to
// config.Load while a snapshot exists, so the migration has to run here too.
// It is idempotent, so it stops reporting once an update persists the result.
func migrateSnapshotConfig(s *State) []string {
	if s.Config == nil {
		return nil
	}
	warnings := config.MigrateLegacy(s.Config)
	if len(warnings) == 0 {
		// The snapshot already used profiles, so an empty member profile is a
		// deliberate "built-in default", not a pre-profile record to repair.
		return nil
	}
	return append(warnings, backfillMemberProfiles(s)...)
}

// backfillMemberProfiles points members saved before profiles existed at the
// profile their launch settings became. Their engine, model and environment were
// materialised when they were added, but the command is resolved at launch from
// the member's profile name; a member carrying none would run the engine by name
// and silently lose a configured launcher on upgrade. An empty profile name has
// to keep meaning "the built-in default" for a member added without one, so the
// distinction is resolved here, where the legacy snapshot is still recognisable.
//
// A one-time step at a stateVersion boundary would be the usual home for this,
// but migrateLedger runs inside normalizeState, before the configuration is
// migrated: at that point no profile exists yet to point a member at. So it runs
// after the configuration migration instead, and only while that migration still
// changes the snapshot: the same update persists both, so once the snapshot uses
// profiles every empty member profile is one added with the built-in default. Do
// not "move it to the right place" without moving the profiles first.
func backfillMemberProfiles(s *State) []string {
	var warnings []string
	for _, id := range sortedKeys(s.Members) {
		m := s.Members[id]
		if m.Profile != "" || m.Engine == "" {
			continue
		}
		name := s.Config.LaunchProfileFor(m.Engine, id == "master")
		if name == "" {
			continue
		}
		m.Profile = name
		warnings = append(warnings, fmt.Sprintf("member %s adopted profiles.%s, which its launch settings migrated to", id, name))
	}
	return warnings
}

// refreshProfileTables adopts the current profile tables into the team snapshot.
// Only the profiles and the two pointers follow the configuration file: the
// engine, model and environment a member was added with stay snapshotted, and
// its command is resolved through its profile at every launch, so an edited or
// newly added profile reaches the next add, restart or resume.
func refreshProfileTables(s *State, current config.Config) {
	if s.Config == nil {
		s.Config = &current
		return
	}
	s.Config.Profiles = current.Profiles
	s.Config.DefaultProfile, s.Config.MasterProfile = current.DefaultProfile, current.MasterProfile
}

// liveConfig is the team snapshot with its profile tables taken from the current
// configuration, so a profile added while the team runs can be used at once. A
// configuration that no longer loads must not stop a running team: the saved
// profiles are kept and the reason is returned as a warning.
func (s *State) liveConfig() (config.Config, string, error) {
	cfg, err := s.effectiveConfig()
	if err != nil {
		return cfg, "", err
	}
	current, err := config.Load(s.Root)
	if err != nil {
		return cfg, profileLoadWarning(err), nil
	}
	refreshProfileTables(&State{Config: &cfg}, current)
	return cfg, "", nil
}

func profileLoadWarning(err error) string {
	return fmt.Sprintf("keeping the team's saved profiles because the configuration did not load: %v", err)
}

// refreshProfiles persists the live profile tables into the snapshot before a
// member is added or launched, so the launch, and the helper commands that later
// resolve through the same snapshot, all use the profile the member was given.
func (st *Store) refreshProfiles() (*State, error) { return st.loadProfiles(false) }

// requireProfiles is refreshProfiles for a caller that asked to re-read the
// configuration: falling back to the snapshot would silently ignore the edit
// the caller wants applied, so a configuration that does not load is an error.
func (st *Store) requireProfiles() (*State, error) { return st.loadProfiles(true) }

func (st *Store) loadProfiles(required bool) (*State, error) {
	s, err := st.read()
	if err != nil {
		return nil, err
	}
	current, err := config.Load(s.Root)
	if err != nil && required {
		return nil, fmt.Errorf("cannot re-read profiles; nothing was stopped or changed: %w", err)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Profile warning:", profileLoadWarning(err))
		return s, nil
	}
	if err = st.update(func(s *State) error { refreshProfileTables(s, current); return nil }); err != nil {
		return nil, err
	}
	return st.read()
}

// effectiveConfig keeps running teams on their startup snapshot, including env overrides.
func (s *State) effectiveConfig() (config.Config, error) {
	if s.Config != nil {
		return *s.Config, nil
	}
	return config.Load(s.Root)
}

// engineHelper uses the team's command snapshot for native helper subcommands.
func (s *State) engineHelper(m *Member, args ...string) (string, error) {
	cfg, err := s.effectiveConfig()
	if err != nil {
		return "", err
	}
	command, _ := cfg.ProfileCommand(m.Profile, m.Engine)
	name, argv, env := command.Invocation(m.Env, "", args...)
	return process.RunEnv(m.Cwd, env, name, argv...)
}

// profileListing is what an agent needs to choose a profile for member add. The
// environment is reduced to its variable names: a profile commonly carries an
// account directory or a token, and none of that belongs in an agent's context.
type profileListing struct {
	DefaultProfile string           `json:"default_profile"`
	MasterProfile  string           `json:"master_profile"`
	Profiles       []profileSummary `json:"profiles"`
}

type profileSummary struct {
	Name          string        `json:"name"`
	Engine        config.Engine `json:"engine"`
	Model         string        `json:"model,omitempty"`
	CustomCommand bool          `json:"custom_command"`
	EnvKeys       []string      `json:"env_keys,omitempty"`
}

func newProfileListing(cfg config.Config) profileListing {
	listing := profileListing{DefaultProfile: cfg.DefaultProfile, MasterProfile: cfg.MasterProfile, Profiles: []profileSummary{}}
	for _, name := range sortedKeys(cfg.Profiles) {
		p := cfg.Profiles[name]
		listing.Profiles = append(listing.Profiles, profileSummary{Name: name, Engine: p.Engine, Model: p.Model, CustomCommand: p.Command != nil, EnvKeys: sortedKeys(p.Env)})
	}
	return listing
}
