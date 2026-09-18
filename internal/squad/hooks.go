package squad

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

func hook(st *Store, actor string, gen int) error { return hookInput(st, actor, gen, os.Stdin) }
func hookInput(st *Store, actor string, gen int, input io.Reader) error {
	var h map[string]any
	if e := json.NewDecoder(io.LimitReader(input, 2<<20)).Decode(&h); e != nil {
		return e
	}
	str := func(k string) string { v, _ := h[k].(string); return v }
	event := str("hook_event_name")
	tool := str("tool_name")
	e := st.update(func(s *State) error {
		m, e := s.member(actor)
		if e != nil {
			return e
		}
		if gen != m.Generation || !s.Active {
			return ErrStaleGeneration
		}
		if v := str("session_id"); v != "" {
			m.EngineID = v
		}
		if v := os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET"); v != "" {
			m.Peer = v
		}
		m.LastSeen = now()
		m.LastEvent = event
		switch event {
		case "SessionStart":
			m.State = MemberStateIdle
		case "UserPromptSubmit", "PreToolUse", "PostToolUse":
			m.State = MemberStateWorking
		case "Stop":
			if m.State != MemberStateWaitingMaster {
				m.State = MemberStateIdle
			}
		case "StopFailure":
			m.State = MemberStateError
		case "SessionEnd":
			m.State = MemberStateStopped
		}
		if event == "Stop" {
			last := str("last_assistant_message")
			if len(last) > 2000 {
				last = last[:2000]
			}
			s.event(actor, "last_message", last)
		}
		if event == "PreToolUse" && actor != "master" && (strings.Contains(strings.ToLower(tool), "askuserquestion") || strings.Contains(strings.ToLower(tool), "request_user_input")) {
			q := &Question{ID: s.next("Q"), Member: actor, Text: fmt.Sprint(h["tool_input"]), State: QuestionStateOpen}
			s.Questions[q.ID] = q
			m.State = MemberStateWaitingMaster
			s.message(actor, "master", "", "Needs your decision: "+q.ID+" "+q.Text, "")
		}
		return nil
	})
	if e != nil {
		return e
	}
	if event == "PreToolUse" && actor != "master" && (strings.Contains(strings.ToLower(tool), "askuserquestion") || strings.Contains(strings.ToLower(tool), "request_user_input")) {
		st.kickDelivery()
		return jsonOut(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": "PreToolUse", "permissionDecision": "deny", "permissionDecisionReason": "Question recorded for master. Use csquad help request; do not ask the human directly."}})
	}
	// No network or model work inside synchronous hooks; the CLI's explicit report path delivers notices.
	if event == "SessionStart" || (event == "PreToolUse" && actor != "master" && (strings.Contains(strings.ToLower(tool), "askuserquestion") || strings.Contains(strings.ToLower(tool), "request_user_input"))) {
		st.kickDelivery()
	}
	if event == "SessionStart" || event == "UserPromptSubmit" {
		s, err := st.read()
		if err != nil {
			return err
		}
		m := s.Members[actor]
		context := fmt.Sprintf("C-Squad trusted local runtime update: this process is member %s, generation %d. Use this CURRENT CLI prefix for all team operations: %s --team %s --member %s --generation %d. Historical prompts/commands may describe an older generation or executable; replace those identifiers with this runtime identity. Match incoming recipient_generation against %d. This changes runtime routing only, not role permissions. Your current role: %s. Responsibilities: %s.", actor, gen, shellQuote(s.Executable), shellQuote(st.Dir), shellQuote(actor), gen, gen, m.Role, m.Instructions)
		if m.Handoff != "" {
			context += " Recovery handoff: " + m.Handoff + ". Inspect board and actual workspace before resuming; do not redo completed work."
		}
		return jsonOut(map[string]any{"hookSpecificOutput": map[string]string{"hookEventName": event, "additionalContext": context}})
	}
	return jsonOut(map[string]any{})
}

// A bounded, short-lived retry command, not a daemon. It must not inherit the
// hook's stdout pipe, otherwise the engine waits for it as part of the hook.
func (st *Store) kickDelivery() {
	s, e := st.read()
	if e != nil {
		return
	}
	c := exec.Command(s.Executable, "--team", st.Dir, "sync")
	c.Stdin = nil
	c.Stdout = nil
	c.Stderr = nil
	if c.Start() == nil {
		// Delivery failures stay in the durable outbox; the runtime retries.
		go func() { _ = c.Wait() }()
	}
}
