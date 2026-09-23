package squad

import (
	"strings"
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"
)

// profileConfig is the shape the finalised design documents: two profiles, a
// pointer for each role, and a wrapper command on one of them.
func profileConfig() config.Config {
	cfg := config.Defaults()
	cfg.Profiles = map[string]config.Profile{
		"std": {Engine: config.Claude, Model: "sonnet", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/std"}},
		"pro": {
			Engine:  config.Claude,
			Model:   "opus",
			Env:     map[string]string{"CLAUDE_CONFIG_DIR": "/pro", "SHARED": "profile"},
			Command: &config.Command{Executable: "claude-pro", Args: []string{"--pro"}},
		},
	}
	cfg.DefaultProfile, cfg.MasterProfile = "std", "pro"
	return cfg
}

// Test 1: the profile is the whole launch definition. Engine, model, environment
// and command all come from it, and no command line value can replace a part of
// it, because every flag that used to do so is gone.
func TestProfileSuppliesTheWholeLaunch(t *testing.T) {
	cfg := profileConfig()
	p, name, err := cfg.ResolveProfile("pro", false)
	must(t, err)
	if name != "pro" || p.Engine != config.Claude || p.Model != "opus" {
		t.Fatalf("profile not applied: %q %+v", name, p)
	}
	env := profileEnv(p)
	if env["CLAUDE_CONFIG_DIR"] != "/pro" || env["SHARED"] != "profile" {
		t.Fatalf("profile environment not applied: %+v", env)
	}
	command, warning := cfg.ProfileCommand(name, p.Engine)
	if command.Executable != "claude-pro" || warning != "" {
		t.Fatalf("profile command not applied: %+v %q", command, warning)
	}
	for _, removed := range []options{{"engine": "codex"}, {"model": "haiku"}, {"env": "CODEX_HOME=/x"}, {"role": "reviewer"}} {
		err = rejectRemovedLaunchFlags(removed)
		if err == nil || !strings.Contains(err.Error(), "was removed") || !strings.Contains(err.Error(), "profile") && !strings.Contains(err.Error(), "--instructions") {
			t.Fatalf("%+v was accepted or not redirected at a profile: %v", removed, err)
		}
	}
}

// Test 10 and 11, command-line half: each role follows its own pointer, and an
// explicit name overrides it for that launch only.
func TestProfilePointersAndExplicitSelection(t *testing.T) {
	cfg := profileConfig()
	worker, name, err := cfg.ResolveProfile("", false)
	must(t, err)
	if name != "std" || worker.Model != "sonnet" {
		t.Fatalf("default_profile not applied to a member: %q %+v", name, worker)
	}
	master, name, err := cfg.ResolveProfile("", true)
	must(t, err)
	if name != "pro" || master.Model != "opus" {
		t.Fatalf("master_profile not applied to Master: %q %+v", name, master)
	}
	if _, name, err = cfg.ResolveProfile("std", true); err != nil || name != "std" {
		t.Fatalf("start --profile did not override master_profile: %q %v", name, err)
	}
	if _, name, err = cfg.ResolveProfile("pro", false); err != nil || name != "pro" {
		t.Fatalf("member add --profile did not override default_profile: %q %v", name, err)
	}
	// Test 4 on the launch path: an unknown name never falls back silently.
	if _, _, err = cfg.ResolveProfile("typo", false); err == nil || !strings.Contains(err.Error(), "available profiles: pro, std") {
		t.Fatalf("unknown --profile: %v", err)
	}
}

// Test 3 on the launch path: a member records the profile its launch command is
// resolved through, including one selected by a pointer rather than a flag.
// A profile that defines no command launches the engine by name.
func TestMemberRecordsResolvedProfileForCommandLookup(t *testing.T) {
	cfg := profileConfig()
	_, master, err := cfg.ResolveProfile("", true)
	must(t, err)
	command, warning := cfg.ProfileCommand(master, config.Claude)
	if command.Executable != "claude-pro" || warning != "" {
		t.Fatalf("a pointer-selected profile lost its command: %+v %q", command, warning)
	}
	_, worker, err := cfg.ResolveProfile("", false)
	must(t, err)
	if command, warning = cfg.ProfileCommand(worker, config.Claude); command.Executable != string(config.Claude) || warning != "" {
		t.Fatalf("a profile without a command must run the engine by name: %+v %q", command, warning)
	}
}

// Test 7: the member outlives the profile. A launch after the profile is removed
// falls back to the built-in default and records a warning instead of failing.
func TestRemovedProfileStillLaunches(t *testing.T) {
	st := testStore(t)
	cfg := profileConfig()
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["a"].Profile = "pro"
		s.Members["a"].Engine = config.Claude
		return nil
	}))
	s, err := st.read()
	must(t, err)
	live, err := s.effectiveConfig()
	must(t, err)
	if command, _ := live.ProfileCommand(s.Members["a"].Profile, s.Members["a"].Engine); command.Executable != "claude-pro" {
		t.Fatalf("profile command not used while the profile exists: %+v", command)
	}
	// The user edits the configuration and a resume adopts the edit.
	edited := profileConfig()
	delete(edited.Profiles, "pro")
	edited.MasterProfile = ""
	must(t, st.update(func(s *State) error {
		refreshResumeDefaults(s, edited)
		return nil
	}))
	s, err = st.read()
	must(t, err)
	live, err = s.effectiveConfig()
	must(t, err)
	command, warning := live.ProfileCommand(s.Members["a"].Profile, s.Members["a"].Engine)
	if command.Executable != string(config.Claude) {
		t.Fatalf("a removed profile must fall back, not fail: %+v", command)
	}
	if !strings.Contains(warning, "pro") {
		t.Fatalf("a removed profile must warn: %q", warning)
	}
	if s.Members["a"].Engine != config.Claude {
		t.Fatal("removing a profile changed an existing member's engine")
	}
}

// Test 8: engine, model and environment are materialised when the member is
// added, so later configuration edits never reach an existing member. Only the
// profile table itself is refreshed on resume, because the command is resolved
// at launch.
func TestSnapshotKeepsMembersOnTheirLaunchSettings(t *testing.T) {
	st := testStore(t)
	cfg := profileConfig()
	p, name, err := cfg.ResolveProfile("std", false)
	must(t, err)
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["a"] = &Member{ID: "a", Engine: p.Engine, Model: p.Model, Profile: name, Env: profileEnv(p), State: MemberStateIdle, Generation: 1}
		return nil
	}))
	edited := profileConfig()
	edited.Profiles["std"] = config.Profile{Engine: config.Codex, Model: "changed", Env: map[string]string{"CLAUDE_CONFIG_DIR": "/changed"}}
	must(t, st.update(func(s *State) error {
		refreshResumeDefaults(s, edited)
		return nil
	}))
	s, err := st.read()
	must(t, err)
	m := s.Members["a"]
	if m.Engine != config.Claude || m.Model != "sonnet" || m.Env["CLAUDE_CONFIG_DIR"] != "/std" {
		t.Fatalf("an existing member followed a configuration edit: %+v", m)
	}
	// The refreshed table is what a launch resolves the command through.
	live, err := s.effectiveConfig()
	must(t, err)
	if live.Profiles["std"].Model != "changed" {
		t.Fatalf("resume did not adopt the edited profiles: %+v", live.Profiles)
	}
}

// Test 17: a team created before profiles existed keeps a snapshot with legacy
// engine fields, and that snapshot never passes through config.Load. Migrating
// it on the read path keeps such a team on the engine it was started with.
func TestLegacySnapshotMigratesOnRead(t *testing.T) {
	st := testStore(t)
	legacy := config.Defaults()
	legacy.Engine, legacy.Model = config.Claude, "sonnet"
	legacy.MasterEngine = config.Codex
	must(t, st.update(func(s *State) error {
		s.Config = &legacy
		return nil
	}))
	s, err := st.read()
	must(t, err)
	cfg, err := s.effectiveConfig()
	must(t, err)
	worker, _, err := cfg.ResolveProfile("", false)
	must(t, err)
	if worker.Engine != config.Claude || worker.Model != "sonnet" {
		t.Fatalf("an upgraded team fell back to the built-in worker default: %+v", worker)
	}
	master, _, err := cfg.ResolveProfile("", true)
	must(t, err)
	if master.Engine != config.Codex {
		t.Fatalf("an upgraded team fell back to the built-in master default: %+v", master)
	}
	// The migration is recorded once, on the path that persists it.
	var migrations int
	for _, event := range s.Events {
		if event.Kind == "config_migrated" {
			migrations++
		}
	}
	if migrations != 0 {
		t.Fatalf("the read path recorded %d unpersisted migration events", migrations)
	}
	must(t, st.update(func(s *State) error { return nil }))
	s, err = st.read()
	must(t, err)
	migrations = 0
	for _, event := range s.Events {
		if event.Kind == "config_migrated" {
			migrations++
		}
	}
	if migrations == 0 {
		t.Fatal("the migration left no trace in the ledger")
	}
	must(t, st.update(func(s *State) error { return nil }))
	s, err = st.read()
	must(t, err)
	repeated := 0
	for _, event := range s.Events {
		if event.Kind == "config_migrated" {
			repeated++
		}
	}
	if repeated != migrations {
		t.Fatalf("the migration ran again: %d then %d", migrations, repeated)
	}
}

// A member added before profiles existed carries no profile name, and the launch
// command is the one setting resolved from the profile at every start rather than
// snapshotted onto the member. Without a backfill such a member would silently
// run the engine by name and lose the launcher the team was configured with, so
// the snapshot migration points it at the profile its engine command became.
func TestLegacyMembersKeepTheirLauncherAcrossMigration(t *testing.T) {
	st := testStore(t)
	legacy := config.Defaults()
	legacy.Engine = config.Claude
	legacy.EngineCommands = map[config.Engine]config.Command{
		config.Claude: {Executable: "/legacy/bin/claude-wrapper", Args: []string{"--fixed"}},
		config.Codex:  {Executable: "/legacy/bin/codex-wrapper"},
	}
	must(t, st.update(func(s *State) error {
		s.Config = &legacy
		s.Members["b"].Engine = config.Codex
		return nil
	}))
	s, err := st.read()
	must(t, err)
	cfg, err := s.effectiveConfig()
	must(t, err)
	for id, want := range map[string]string{"master": "/legacy/bin/claude-wrapper", "a": "/legacy/bin/claude-wrapper", "b": "/legacy/bin/codex-wrapper"} {
		m := s.Members[id]
		if m.Profile == "" {
			t.Fatalf("member %s was left without a profile to resolve its command through", id)
		}
		command, warning := cfg.ProfileCommand(m.Profile, m.Engine)
		if command.Executable != want || warning != "" {
			t.Fatalf("member %s lost its configured launcher: %+v %q", id, command, warning)
		}
	}
	// Migration does not move a member that already names a profile.
	must(t, st.update(func(s *State) error {
		s.Members["a"].Profile = "chosen"
		return nil
	}))
	s, err = st.read()
	must(t, err)
	if s.Members["a"].Profile != "chosen" {
		t.Fatalf("an explicit profile was replaced: %q", s.Members["a"].Profile)
	}
}

// Test 6: the deprecated coupling stays severed. A profile name is inert: a
// member whose own name matches one gets nothing injected from it and is not
// launched with it.
func TestProfileNameMatchingAMemberInjectsNothing(t *testing.T) {
	st := testStore(t)
	cfg := config.Defaults()
	cfg.Profiles = map[string]config.Profile{"a": {Engine: config.Claude, Model: "opus"}}
	cfg.Templates = map[string]config.Template{"a": {Prompt: "secret injected responsibilities"}}
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["a"].Instructions = "own responsibilities"
		return nil
	}))
	s, err := st.read()
	must(t, err)
	m := s.Members["a"]
	for _, render := range []func(*State, *Member) (string, error){prompt, runtimePrompt} {
		text, err := render(s, m)
		must(t, err)
		if strings.Contains(text, "secret injected responsibilities") {
			t.Fatal("a configured table was injected by matching the member's name")
		}
		if !strings.Contains(text, "own responsibilities") {
			t.Fatal("the member's own responsibilities are missing")
		}
	}
	// Matching a profile name does not select that profile either: with no
	// pointer configured, the member still gets the built-in worker default.
	p, name, err := s.Config.ResolveProfile("", false)
	must(t, err)
	if name != "" || p.Engine != config.Codex {
		t.Fatalf("a profile was selected by the member's name: %q %+v", name, p)
	}
}

// A member added with the built-in default records no profile. Once the snapshot
// uses profiles that empty name is deliberate, so the pre-profile backfill must
// not bind it to whichever profile happens to launch the same engine.
func TestBuiltinDefaultMembersAreNotBoundToAProfile(t *testing.T) {
	st := testStore(t)
	cfg := config.Defaults()
	cfg.DefaultProfile, cfg.MasterProfile = "", ""
	cfg.Profiles = map[string]config.Profile{
		"work": {Engine: config.Codex, Command: &config.Command{Executable: "/opt/work/codex-wrapper"}},
	}
	must(t, st.update(func(s *State) error {
		s.Config = &cfg
		s.Members["b"].Engine, s.Members["b"].Profile = config.Codex, ""
		return nil
	}))
	s, err := st.read()
	must(t, err)
	if profile := s.Members["b"].Profile; profile != "" {
		t.Fatalf("a built-in default member was bound to profiles.%s", profile)
	}
}
