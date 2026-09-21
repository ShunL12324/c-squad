package squad

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/process"
	"github.com/ShunL12324/c-squad/internal/tmux"

	_ "modernc.org/sqlite"
)

// Member records an agent identity, its launch configuration, and observed runtime state.
// Generation fences writes from previous incarnations of the same member.
type Member struct {
	Color        tmux.Color         `json:"color,omitempty"`
	Instructions string             `json:"instructions,omitempty"`
	Env          map[string]string  `json:"env,omitempty"`
	ID           string             `json:"id"`
	Engine       config.Engine      `json:"engine"`
	Model        string             `json:"model,omitempty"`
	Role         string             `json:"role"`
	Session      string             `json:"tmux_session"`
	Pane         string             `json:"pane,omitempty"`
	EngineID     string             `json:"engine_id,omitempty"`
	Peer         string             `json:"peer_socket,omitempty"`
	Cwd          string             `json:"cwd"`
	State        MemberState        `json:"state"`
	Generation   int                `json:"generation"`
	LastEvent    string             `json:"last_event,omitempty"`
	LastSeen     string             `json:"last_seen,omitempty"`
	ObservedAt   string             `json:"observed_at,omitempty"`
	RunnerPID    int                `json:"runner_pid,omitempty"`
	EnginePID    int                `json:"engine_pid,omitempty"`
	Processes    []process.Identity `json:"processes,omitempty"`
	ProcessStart string             `json:"process_start,omitempty"`
	Handoff      string             `json:"handoff,omitempty"`
}

// Milestone tracks a reporting checkpoint and whether master approval gates further work.
type Milestone struct {
	Name  string         `json:"name"`
	Gate  bool           `json:"gate"`
	State MilestoneState `json:"state"`
}

// Task records ownership, workspace, progress, and delivery evidence.
// State is the workflow phase; Blockers independently describe why work cannot proceed.
type Task struct {
	ID                 string       `json:"id"`
	Title              string       `json:"title"`
	Description        string       `json:"description"`
	Acceptance         string       `json:"acceptance"`
	State              TaskPhase    `json:"state"`
	Dispatch           DispatchMode `json:"dispatch"`
	RequestKey         string       `json:"request_key,omitempty"`
	Blockers           []string     `json:"blockers"`
	MergeIntent        *Approval    `json:"merge_intent,omitempty"`
	Setup              string       `json:"setup,omitempty"`
	Owner              string       `json:"owner,omitempty"`
	Participants       []string     `json:"participants"`
	Dependencies       []string     `json:"dependencies"`
	Workspace          string       `json:"workspace,omitempty"`
	Branch             string       `json:"branch,omitempty"`
	Base               string       `json:"base,omitempty"`
	Target             string       `json:"target,omitempty"`
	Progress           string       `json:"progress,omitempty"`
	Updated            string       `json:"updated"`
	Candidate          string       `json:"candidate,omitempty"`
	CandidateAuthor    string       `json:"candidate_author,omitempty"`
	Submission         string       `json:"submission,omitempty"`
	SubmissionRevision uint64       `json:"submission_revision,omitempty"`
	SubmissionSummary  string       `json:"submission_summary,omitempty"`
	Milestones         []Milestone  `json:"milestones"`
	Evidence           []Evidence   `json:"evidence"`
	Approval           *Approval    `json:"approval,omitempty"`
	MergeCommit        string       `json:"merge_commit,omitempty"`
	// ExternalClosure is set only by task close-external and never alongside MergeCommit.
	ExternalClosure *ExternalClosure `json:"external_closure,omitempty"`
}

// ExternalClosure records a master decision to close a code task on a commit that
// lives in a repository the team does not own. It is never a merge: MergeCommit
// stays empty, no object is imported, and Limits states what was not verified.
type ExternalClosure struct {
	Repo      string `json:"repo"`
	GitDir    string `json:"git_dir"`
	SHA       string `json:"sha"`
	Subject   string `json:"subject,omitempty"`
	Reason    string `json:"reason"`
	Summary   string `json:"summary,omitempty"`
	Limits    string `json:"limits"`
	By        string `json:"by"`
	At        string `json:"at"`
	Workspace string `json:"workspace,omitempty"`
	Branch    string `json:"branch,omitempty"`
}

// Evidence records a result for an immutable submission and, for code, its exact commit.
type Evidence struct {
	Member     string       `json:"member"`
	Kind       EvidenceKind `json:"kind"`
	SHA        string       `json:"sha,omitempty"`
	Submission string       `json:"submission,omitempty"`
	Passed     bool         `json:"passed"`
	Summary    string       `json:"summary"`
}

// Approval binds master approval to both the candidate and target commits.
type Approval struct {
	SHA       string `json:"sha"`
	TargetSHA string `json:"target_sha"`
	By        string `json:"by"`
}

// UserSender marks a message the human raised from a panel rather than an agent.
// It is a reserved sender identity only: no Member carries it, it is never a CLI
// actor, and nothing can be delivered to it. Member IDs reserve it so the origin
// of such a message cannot be forged by recruiting a member of the same name.
const UserSender = "user"

// Message is a durable outbox entry retained until the recipient acknowledges it.
// Attempt and RecipientGeneration fence delivery retries across member restarts.
type Message struct {
	ID                  string        `json:"id"`
	From                string        `json:"from"`
	To                  string        `json:"to"`
	Task                string        `json:"task,omitempty"`
	Text                string        `json:"text"`
	ReplyTo             string        `json:"reply_to,omitempty"`
	State               DeliveryState `json:"state"`
	Error               string        `json:"error,omitempty"`
	Created             string        `json:"created"`
	Attempt             string        `json:"attempt,omitempty"`
	Attempts            int           `json:"attempts"`
	RecipientGeneration int           `json:"recipient_generation,omitempty"`
	RequestKey          string        `json:"request_key,omitempty"`
}

// Question records a member request to master and its blocking answer state.
type Question struct {
	ID     string        `json:"id"`
	Member string        `json:"member"`
	Task   string        `json:"task"`
	Text   string        `json:"text"`
	Answer string        `json:"answer,omitempty"`
	State  QuestionState `json:"state"`
}

// Event records a timestamped team activity for the bounded audit history.
type Event struct {
	At     string `json:"at"`
	Member string `json:"member"`
	Kind   string `json:"kind"`
	Text   string `json:"text"`
}

// State is the persisted team ledger shared by CLI commands and the runtime.
// Mutations must use Store.update so generation checks and transactions apply.
type State struct {
	StartupOverrides *map[string]string   `json:"startup_overrides,omitempty"`
	PanelView        panelView            `json:"panel_view,omitempty"`
	Epoch            int                  `json:"epoch,omitempty"`
	Phase            TeamPhase            `json:"phase,omitempty"`
	OwnSocket        bool                 `json:"own_socket,omitempty"`
	StopReason       string               `json:"stop_reason,omitempty"`
	Version          int                  `json:"version"`
	ID               string               `json:"id"`
	Root             string               `json:"root"`
	Socket           string               `json:"tmux_socket"`
	Executable       string               `json:"executable"`
	Active           bool                 `json:"active"`
	Members          map[string]*Member   `json:"members"`
	Tasks            map[string]*Task     `json:"tasks"`
	Messages         []*Message           `json:"messages"`
	Questions        map[string]*Question `json:"questions"`
	Events           []Event              `json:"events"`
	Sequence         int                  `json:"sequence"`
	Config           *config.Config       `json:"config,omitempty"`
	RuntimeSeen      string               `json:"runtime_seen,omitempty"`
}

func now() string { return time.Now().UTC().Format(time.RFC3339Nano) }
func (s *State) next(prefix string) string {
	s.Sequence++
	return fmt.Sprintf("%s%d", prefix, s.Sequence)
}
func (s *State) event(member, kind, text string) {
	s.Events = append(s.Events, Event{now(), member, kind, text})
	if len(s.Events) > 300 {
		s.Events = s.Events[len(s.Events)-300:]
	}
}
func (s *State) message(from, to, task, text, reply string) *Message {
	m := &Message{ID: s.next("M"), From: from, To: to, Task: task, Text: text, ReplyTo: reply, State: DeliveryStatePending, Created: now()}
	s.Messages = append(s.Messages, m)
	return m
}
func (s *State) task(id string) (*Task, error) {
	t := s.Tasks[id]
	if t == nil {
		return nil, fmt.Errorf("task %q: %w", id, ErrNotFound)
	}
	return t, nil
}
func (s *State) member(id string) (*Member, error) {
	m := s.Members[id]
	if m == nil {
		return nil, fmt.Errorf("member %q: %w", id, ErrNotFound)
	}
	return m, nil
}

// Store holds the ledger connection and the caller identity used to fence writes.
type Store struct {
	Dir        string
	DB         *sql.DB
	Actor      string
	Generation int
}

func openStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("no team selected; use --team <state-directory> or start a team")
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "state.db")+"?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(`CREATE TABLE IF NOT EXISTS state (id INTEGER PRIMARY KEY CHECK(id=1), data TEXT NOT NULL)`); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err = os.Chmod(filepath.Join(dir, "state.db"), 0600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Store{Dir: dir, DB: db}, nil
}
func (st *Store) update(fn func(*State) error) error {
	c, err := st.DB.Conn(context.Background())
	if err != nil {
		return err
	}
	defer func() { _ = c.Close() }()
	if _, err = c.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		return err
	}
	// COMMIT is checked below; rollback after commit may report no active transaction.
	defer func() { _, _ = c.ExecContext(context.Background(), "ROLLBACK") }()
	var raw string
	var s State
	err = c.QueryRowContext(context.Background(), "SELECT data FROM state WHERE id=1").Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if raw != "" {
		if err = json.Unmarshal([]byte(raw), &s); err != nil {
			return err
		}
	}
	if st.Generation > 0 {
		m := s.Members[st.Actor]
		if !s.Active || m == nil || m.Generation != st.Generation {
			return ErrStaleGeneration
		}
	}
	normalizeState(&s)
	if err = fn(&s); err != nil {
		return err
	}
	normalizeState(&s)
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if _, err = c.ExecContext(context.Background(), "INSERT INTO state(id,data) VALUES(1,?) ON CONFLICT(id) DO UPDATE SET data=excluded.data", string(b)); err != nil {
		return err
	}
	_, err = c.ExecContext(context.Background(), "COMMIT")
	return err
}
func (st *Store) read() (*State, error) {
	var raw string
	if err := st.DB.QueryRow("SELECT data FROM state WHERE id=1").Scan(&raw); err != nil {
		return nil, err
	}
	var s State
	err := json.Unmarshal([]byte(raw), &s)
	normalizeState(&s)
	return &s, err
}

// Blockers describe independent reasons to wait. They never replace a task phase.
func normalizeState(s *State) {
	for _, t := range s.Tasks {
		if t.Dispatch == "" {
			t.Dispatch = DispatchModeAssigned
		}
		if t.State == TaskPhaseBlocked { // migrate the original prototype conservatively
			if t.Approval != nil {
				t.State = TaskPhaseAwaitingMerge
			} else if t.Candidate != "" {
				t.State = TaskPhaseInReview
			} else {
				t.State = TaskPhaseInProgress
			}
		}
		migrateSubmission(t)
		t.Blockers = []string{}
		if t.State == TaskPhaseDone {
			continue
		}
		for _, d := range t.Dependencies {
			if dep := s.Tasks[d]; dep == nil || dep.State != TaskPhaseDone {
				t.Blockers = append(t.Blockers, "dependency:"+d)
			}
		}
		for _, m := range t.Milestones {
			if m.State == MilestoneStateAwaitingApproval {
				t.Blockers = append(t.Blockers, "gate:"+m.Name)
			}
		}
		for _, q := range s.Questions {
			if q.Task == t.ID && q.State == QuestionStateOpen {
				t.Blockers = append(t.Blockers, "question:"+q.ID)
			}
		}
		if t.Owner == "" && t.State != TaskPhaseReady {
			t.Blockers = append(t.Blockers, "owner:unassigned")
		}
		sort.Strings(t.Blockers)
	}
}

func canOwn(s *State, task *Task, id string) error {
	m, e := s.member(id)
	if e != nil {
		return e
	}
	if m.State == MemberStateRemoved || m.State == MemberStateStopping || m.State == MemberStateNeedsAttention {
		return fmt.Errorf("member unavailable: %s", id)
	}
	for _, t := range s.Tasks {
		if t.ID != task.ID && t.Owner == id && t.State != TaskPhaseDone {
			return fmt.Errorf("%s already owns unfinished task %s; finish or hand off first", id, t.ID)
		}
	}
	return nil
}
