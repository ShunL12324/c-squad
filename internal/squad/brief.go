package squad

import (
	"encoding/json"
	"fmt"
	"net"
	"time"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/filelock"
	"github.com/ShunL12324/c-squad/internal/prompts"
)

// legacyBrief identifies historical outbox requests solely to prevent replay.
// Their records remain intact for audit; new requests never enter the outbox.
func legacyBrief(m *Message) bool {
	return m.From == UserSender && m.To == "master" && m.RequestKey == UserSender+":brief:"+m.Task
}

func briefCommand(st *Store, actor, id string) error {
	note, err := briefRequest(st, actor, id)
	if err != nil {
		return err
	}
	return jsonOut(map[string]string{"task": id, "note": note})
}

// briefRequest sends one native user input, without mutating task or message state.
// Lifecycle locks keep the checked master incarnation stable during transport.
func briefRequest(st *Store, actor, id string) (string, error) {
	if actor != "master" {
		return "", fmt.Errorf("only master may request a brief report: %w", ErrMasterRequired)
	}
	before, err := st.read()
	if err != nil {
		return "", err
	}
	prior := before.Members["master"]
	if prior == nil {
		return "", fmt.Errorf("no master session to ask")
	}
	teamUnlock, err := filelock.Acquire(st.Dir, "team-lifecycle", false)
	if err != nil {
		return "", err
	}
	defer teamUnlock()
	unlock, err := filelock.Acquire(st.Dir, "member-master", false)
	if err != nil {
		return "", err
	}
	defer unlock()
	s, err := st.read()
	if err != nil {
		return "", err
	}
	if !s.Active {
		return "", ErrTeamStopped
	}
	m := s.Members["master"]
	if m == nil || m.Generation != prior.Generation {
		return "", ErrStaleGeneration
	}
	if st.Generation > 0 {
		caller := s.Members[st.Actor]
		if caller == nil || caller.Generation != st.Generation {
			return "", ErrStaleGeneration
		}
	}
	if _, err = s.task(id); err != nil {
		return "", err
	}
	switch m.State {
	case MemberStateStopped, MemberStateStopping, MemberStateRemoved, MemberStateCrashed, MemberStateNeedsAttention:
		return "", fmt.Errorf("no master session to ask: master is %s", m.State)
	}
	if m.EngineID == "" {
		return "", fmt.Errorf("master native session is not ready; enter a first message and try again")
	}
	if masterGone(s) {
		return "", fmt.Errorf("no master session to ask; recover the team first")
	}
	text, err := prompts.Brief(id)
	if err != nil {
		return "", err
	}
	switch m.Engine {
	case config.Codex:
		// Never bootstrap through send-keys: the attached user's composer is private.
		_, err = s.engineHelper(m, "queue", "--thread", m.EngineID, "--message", text)
	case config.Claude:
		if m.Peer == "" {
			return "", fmt.Errorf("master native inbox is not ready")
		}
		var conn net.Conn
		conn, err = net.DialTimeout("unix", m.Peer, 2*time.Second)
		if err == nil {
			defer func() { _ = conn.Close() }()
			err = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
			if err == nil {
				// Native user frame: no peer sender, cross-session envelope or team metadata.
				err = json.NewEncoder(conn).Encode(map[string]any{"type": "user", "session_id": m.EngineID, "priority": "next", "message": map[string]string{"role": "user", "content": text}})
			}
		}
	default:
		return "", fmt.Errorf("unsupported master engine %q", m.Engine)
	}
	if err != nil {
		return "", fmt.Errorf("brief transport failed; try again: %w", err)
	}
	return "Sent to master's native input", nil
}
