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
