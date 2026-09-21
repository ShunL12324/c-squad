package squad

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

var validID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,40}$`)

// plumbingCommands are the commands whose argv C Squad builds itself and runs
// through tmux, never a command an agent types. This classification is
// load-bearing, not a convenience: panels.go, navigation.go, recovery.go and
// pump.go all pass "--member master --generation 0" into run-shell, and
// run-shell inherits the target session's environment, so those panes name
// master while CSQUAD_MEMBER_ID names the member who owns the pane. Making the
// environment authoritative there would break panels, navigation and shutdown
// for a running team. Every other command is a ledger command: its argv comes
// from an agent or a human, so the session environment wins over the flags.
// Unlisted commands are ledger commands by default; a new command fails closed.
var plumbingCommands = []string{"run-engine", "hook", "ui", "ui-panel", "ui-toggle", "ui-layout", "ui-remember-layout", "navigate", "navigation", "runtime", "shutdown", "sync", "reconcile"}

// cleanPath normalises a directory for comparison and for the prefix injected
// into agents. It keeps an empty value empty, which filepath.Clean would not,
// and deliberately does not resolve symlinks: a Homebrew bin/csquad would
// resolve into a versioned Cellar path that brew cleanup later deletes.
func cleanPath(p string) string {
	if p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// agreement refuses a flag that contradicts the member session it runs inside.
// Repeating the bound value stays legal, which is what keeps teams started
// before this change working: their injected prefix passes exactly these values.
func agreement(kind, flag, env string) error {
	if flag == "" || env == "" || flag == env {
		return nil
	}
	return fmt.Errorf("--%s %q conflicts with this session's %s %q; omit the flag, identity is bound to the session", kind, flag, kind, env)
}

type options map[string]string

func list(s string) []string {
	out := []string{}
	for _, v := range strings.Split(s, ",") {
		if v = strings.TrimSpace(v); v != "" {
			out = append(out, v)
		}
	}
	return out
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// Execute runs a validated command with parsed flags and optional native engine argv.
// The CLI adapter owns syntax, help, and completion; this layer enforces team policy.
func Execute(p []string, values map[string]string, engineArgs []string) error {
	o := options(values)
	if len(p) == 0 {
		p = []string{"start"}
	}
	if p[0] == "version" {
		fmt.Println(buildinfo.String())
		return nil
	}
	if p[0] == "config" {
		c, e := config.Load("")
		if e != nil {
			return e
		}
		return printConfig(c)
	}
	if p[0] == "doctor" {
		return doctor(o)
	}

	if len(p) >= 2 && p[0] == "team" && p[1] == "remove" {
		return removeSavedTeam(o)
	}
	if p[0] == "list" {
		return listTeams(o)
	}
	if p[0] == "start" {
		return start(o)
	}
	ledger := !contains(plumbingCommands, p[0])
	if ledger {
		if e := agreement("team", cleanPath(o["team"]), cleanPath(os.Getenv("CSQUAD_STATE_DIR"))); e != nil {
			return e
		}
		if e := agreement("member", o["member"], os.Getenv("CSQUAD_MEMBER_ID")); e != nil {
			return e
		}
		if e := agreement("generation", o["generation"], os.Getenv("CSQUAD_GENERATION")); e != nil {
			return e
		}
	}
	name := o["team-name"]
	if o["name"] != "" && contains([]string{"resume", "attach", "board", "stop", "ui", "recover"}, p[0]) {
		if name != "" {
			return errors.New("specify only one team selector")
		}
		name = o["name"]
	}
	// Preserve legacy attach MEMBER in explicit/bound context, and attach TEAM
	// outside that context. member attach always names a member unambiguously.
	if p[0] == "attach" && len(p) > 1 && o["team"] == "" && name == "" && os.Getenv("CSQUAD_STATE_DIR") == "" {
		if _, err := namedTeam(p[1]); err == nil {
			name = p[1]
			p = []string{"attach"}
		}
	}
	dir, e := ResolveTeamDirectory(o["team"], name)
	if e != nil {
		return e
	}
	st, e := openStore(dir)
	if e != nil {
		return e
	}
	defer func() { _ = st.DB.Close() }()
	s, e := st.read()
	if e != nil {
		return e
	}
	actor := o["member"]
	if actor == "" {
		actor = os.Getenv("CSQUAD_MEMBER_ID")
	}
	if actor == "" {
		actor = "master"
	}
	genText := o["generation"]
	if genText == "" {
		genText = os.Getenv("CSQUAD_GENERATION")
	}
	gen := 0
	if genText != "" {
		var err error
		gen, err = strconv.Atoi(genText)
		if err != nil || gen < 0 {
			return errors.New("generation must be a nonnegative integer")
		}
	}
	// Generation 0 skips the stale-write check below. That is correct for an
	// outside terminal, which has no incarnation to be stale against, but inside
	// a member session it is a bypass: the caller has a real generation and is
	// discarding it. Plumbing is exempt because C Squad builds those argv itself.
	if ledger && os.Getenv("CSQUAD_MEMBER_ID") != "" && gen == 0 {
		return errors.New("generation 0 is not accepted inside a member session; it would skip the stale-write check")
	}
	if _, err := s.member(actor); err != nil {
		return err
	}
	st.Actor = actor
	st.Generation = gen
	if gen > 0 {
		m, e := s.member(actor)
		if e != nil {
			return e
		}
		if gen != m.Generation || !s.Active {
			return ErrStaleGeneration
		}
	}
	if p[0] == "shutdown" {
		return shutdownRequest(st, o)
	}
	if p[0] == "resume" {
		if actor != "master" || gen != 0 {
			return errors.New("resume the team from an outside terminal")
		}
		return resumeTeam(st, o)
	}
	if p[0] == "run-engine" {
		return runEngine(st, actor, gen, engineArgs)
	}
	if p[0] == "hook" {
		return hook(st, actor, gen)
	}
	if p[0] == "member" && len(p) == 3 && p[1] == "attach" {
		return attach(st, p[2])
	}
	if p[0] == "attach" {
		id := "master"
		if len(p) > 1 {
			id = p[1]
		}
		return attach(st, id)
	}
	if p[0] == "ui" || p[0] == "ui-toggle" {
		return st.openUI(o, p[0] == "ui-toggle")
	}
	if p[0] == "ui-remember-layout" {
		return st.rememberPanelLayout(o["owner"])
	}
	if p[0] == "ui-layout" {
		if o["owner"] != "" {
			return st.configureSelectedPanels(o["owner"])
		}
		return st.configurePanels()
	}
	if p[0] == "ui-panel" {
		return st.runPanel(o["owner"], o["view"], o["popup"] == "true")
	}
	if p[0] == "navigate" {
		return st.navigate(o["client"], o["direction"], o["index"])
	}
	if p[0] == "navigation" {
		return st.configureNavigation()
	}
	if p[0] == "board" {
		if e = st.refresh(); e != nil {
			return e
		}
		s, e = st.read()
		if e != nil {
			return e
		}
		if o["full"] == "true" {
			return queryOut(o, s)
		}
		messages := s.Messages
		if len(messages) > 5 {
			messages = messages[len(messages)-5:]
		}
		events := s.Events
		if len(events) > 10 {
			events = events[len(events)-10:]
		}
		return queryOut(o, map[string]any{"team": s.ID, "root": s.Root, "active": s.Active, "phase": s.Phase, "stop_reason": s.StopReason, "runtime_seen": s.RuntimeSeen, "members": s.Members, "tasks": s.Tasks, "questions": s.Questions, "recent_messages": messages, "recent_activity": events})
	}
	if p[0] == "runtime" {
		return st.runRuntime()
	}
	if p[0] == "reconcile" {
		if actor != "master" {
			return fmt.Errorf("only master reconciles: %w", ErrMasterRequired)
		}
		return st.reconcile()
	}
	if p[0] == "recover" {
		op := "restart"
		if o["fresh"] == "true" {
			op = "replace"
		}
		return lifecycle(st, actor, op, "master", o["prompt"], "")
	}
	if p[0] == "sync" {
		if s.Active {
			if e = st.startRuntime(); e != nil {
				return e
			}
		}
		return st.syncMessages()
	}
	if p[0] == "stop" {
		if actor != "master" {
			return fmt.Errorf("only master can stop team: %w", ErrMasterRequired)
		}
		if gen > 0 {
			cmd := shutdownCommand(st, s, "stopped")
			_, e := tm(s, "run-shell", "-b", cmd)
			return e
		}
		return stop(st)
	}
	if len(p) == 3 && p[0] == "member" && p[1] == "set-cwd" {
		return memberCommand(st, actor, p[1:], o)
	}
	if p[0] == "task" && len(p) > 1 && p[1] == "confirm" {
		return taskCommand(st, actor, p[1:], o)
	}
	if !s.Active {
		return ErrTeamStopped
	}
	if p[0] == "member" {
		return memberCommand(st, actor, p[1:], o)
	}
	if p[0] == "task" {
		return taskCommand(st, actor, p[1:], o)
	}
	if p[0] == "message" || p[0] == "reply" {
		return messageCommand(st, actor, p, o)
	}
	if p[0] == "help" {
		return helpCommand(st, actor, p[1:], o)
	}
	return fmt.Errorf("unknown command; run csquad --help")
}

func start(o options) error {
	cwd, e := os.Getwd()
	if e != nil {
		return e
	}
	root := cwd
	if r, e := git(cwd, "rev-parse", "--show-toplevel"); e == nil {
		root = r
	}
	b := make([]byte, 4)
	if _, e = rand.Read(b); e != nil {
		return e
	}
	id := o["name"]
	if id == "" {
		id = "team-" + hex.EncodeToString(b)
	}
	if !validID.MatchString(id) {
		return fmt.Errorf("invalid team name")
	}
	base := projectBase(root)
	dir := filepath.Join(base, "teams", id)
	if e = rejectExistingTeam(root, dir, id); e != nil {
		return e
	}
	cfg, e := config.Load(root)
	if e != nil {
		return e
	}
	selected := config.EngineDefaults(cfg, true).Engine
	if o["engine"] != "" {
		selected = config.Engine(o["engine"])
	}
	if e = preflight.Check(selected); e != nil {
		return e
	}
	cfg.Env = agentenv.Merge(agentenv.SnapshotSelectors(), cfg.Env)
	startupEnv, e := agentenv.Parse(o["env"])
	if e != nil {
		return e
	}
	cfg.StartupEnv = agentenv.Merge(cfg.StartupEnv, startupEnv)
	if e = excludeProjectState(root); e != nil {
		return e
	}
	if e = os.MkdirAll(base, 0700); e != nil {
		return e
	}
	// Serialize creators before SQLite initialization, which itself writes schema
	// and journal settings. Locking only after openStore can race with SQLITE_BUSY.
	unlock, e := filelock.Acquire(dir, "team-lifecycle", false)
	if e != nil {
		return e
	}
	defer unlock()
	if e = rejectExistingTeam(root, dir, id); e != nil {
		return e
	}
	if e = reapProjectTeams(base); e != nil {
		return e
	}
	st, e := openStore(dir)
	if e != nil {
		return e
	}
	defer func() { _ = st.DB.Close() }()
	if _, err := st.read(); err == nil {
		return errors.New("team already exists; use attach or resume")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	bin, e := os.Executable()
	if e != nil {
		return e
	}
	// The npm launcher execs ../native/<platform>/csquad, so the raw path carries
	// an unnormalised bin/.. segment into every prefix injected into an agent.
	bin = cleanPath(bin)
	socket := filepath.Join(os.TempDir(), fmt.Sprintf("csq-%d-%s.sock", os.Getuid(), hex.EncodeToString(b)))
	if v := os.Getenv("TMUX"); v != "" {
		socket = strings.Split(v, ",")[0]
	}
	t := config.EngineDefaults(cfg, true)
	if o["engine"] != "" {
		t.Engine = config.Engine(o["engine"])
		t.Model = ""
	}
	if o["model"] != "" {
		t.Model = o["model"]
	}
	e = st.update(func(s *State) error {
		*s = State{StartupOverrides: &startupEnv, Version: 2, Epoch: 1, Phase: TeamPhaseStarting, OwnSocket: os.Getenv("TMUX") == "", Config: &cfg, ID: id, Root: root, Socket: socket, Executable: bin, Active: true, Members: map[string]*Member{}, Tasks: map[string]*Task{}, Questions: map[string]*Question{}, Messages: []*Message{}, Events: []Event{}}
		s.Members["master"] = &Member{EnvOverrides: explicitMemberEnv(nil), Color: tmux.Color(o["color"]), Env: memberEnv(cfg, t, nil), ID: "master", Engine: t.Engine, Model: t.Model, Role: "master", Session: "csq-" + id + "-master", Cwd: root, State: MemberStateStarting, Generation: 1}
		return nil
	})
	if e != nil {
		return e
	}
	if e = st.launch("master", false, o["prompt"]); e != nil {
		return errors.Join(e, cleanupTeam(st, "startup_failed"))
	}
	if e = st.update(func(s *State) error { s.Phase = TeamPhaseRunning; return nil }); e != nil {
		return e
	}
	if e = st.startRuntime(); e != nil {
		return errors.Join(e, cleanupTeam(st, "startup_failed"))
	}
	if e = os.WriteFile(filepath.Join(base, "last-team"), []byte(dir), 0600); e != nil {
		return e
	}
	if o["detach"] == "true" {
		return jsonOut(map[string]string{"team": dir, "tmux_socket": socket, "master_session": "csq-" + id + "-master", "attach": bin + " --team " + dir + " attach"})
	}
	unlock()
	return attach(st, "master")
}
func stop(st *Store) error {
	unlock, e := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if e != nil {
		return e
	}
	defer unlock()
	return cleanupTeam(st, "stopped")
}
