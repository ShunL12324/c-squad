package squad

import (
	"testing"

	"github.com/ShunL12324/c-squad/internal/config"

	"github.com/ShunL12324/c-squad/internal/process"
)

func TestRestartPreservesConversationAndFencesOldDelivery(t *testing.T) {
	st := testStore(t)
	must(t, st.update(func(s *State) error {
		m := s.Members["a"]
		m.EngineID = "native-conversation"
		m.Instructions = "review changes"
		m.Env = map[string]string{"CODEX_HOME": "/account-two"}
		m.Pane, m.Peer = "%old", "/old.sock"
		m.RunnerPID, m.EnginePID = 111, 222
		m.Processes = []process.Identity{{PID: 222, Start: "old"}}
		msg := s.message("master", "a", "", "continue review", "")
		msg.State, msg.Error, msg.Attempt = DeliveryStateSending, "old transport error", "old-attempt"
		msg.Attempts = 3
		m.Generation++
		m.resetRuntime()
		msg.recoverDelivery()
		return nil
	}))
	s, err := st.read()
	must(t, err)
	m := s.Members["a"]
	if m.Generation != 2 || m.EngineID != "native-conversation" || m.Instructions != "review changes" || m.Env["CODEX_HOME"] != "/account-two" {
		t.Fatal("restart lost durable identity/configuration")
	}
	if m.Pane != "" || m.Peer != "" || m.RunnerPID != 0 || m.EnginePID != 0 || len(m.Processes) != 0 {
		t.Fatal("restart retained old process/transport identity")
	}
	msg := s.Messages[0]
	if msg.State != DeliveryStatePending || msg.Error != "" || msg.Attempts != 0 || msg.Attempt != "" || msg.Text != "continue review" {
		t.Fatal("restart did not preserve message content and reset its delivery lease")
	}
}

func TestResumeEnvironmentUsesUpdatedDefaults(t *testing.T) {
	// The member still holds the account its profile set, so the edited value
	// replaces it and the old conversation is fenced. A variable the member
	// materialised from somewhere else keeps the value it was added with.
	m := Member{Engine: "codex", EngineID: "old-thread", Env: map[string]string{"CODEX_HOME": "/old", "CUSTOM": "member"}}
	m.applyResumeEnvironment(map[string]string{"CODEX_HOME": "/old", "CUSTOM": "profile"}, map[string]string{"CODEX_HOME": "/new", "CUSTOM": "new-profile"})
	if m.Env["CODEX_HOME"] != "/new" || m.Env["CUSTOM"] != "member" || m.EngineID != "" {
		t.Fatalf("incorrect recovery environment: %+v", m)
	}
	m.EngineID = "new-thread"
	m.applyResumeEnvironment(map[string]string{"CODEX_HOME": "/new"}, map[string]string{"CODEX_HOME": "/new"})
	if m.EngineID != "new-thread" {
		t.Fatal("unchanged account lost its conversation")
	}
}

// A resume adopts an edited profile table and reads the member's environment
// from the profile it was added with, the only layer above the inherited one.
func TestResumeRefreshesProfileConfiguration(t *testing.T) {
	saved := config.Config{Profiles: map[string]config.Profile{
		"pro": {Engine: config.Codex, Env: map[string]string{"CODEX_HOME": "/old", "REMOVED": "old"}}}}
	s := &State{Config: &saved}
	edited := config.Config{Profiles: map[string]config.Profile{
		"pro": {Engine: config.Codex, Env: map[string]string{"CODEX_HOME": "/new"}}}, DefaultProfile: "pro"}
	m := Member{ID: "a", Engine: config.Codex, Profile: "pro", EngineID: "old-session",
		Env: map[string]string{"CODEX_HOME": "/old", "REMOVED": "old", "CUSTOM": "member"}}
	old, current := resumeProfileEnv(s.Config, edited, &m)
	refreshProfileTables(s, edited)
	m.applyResumeEnvironment(old, current)
	if m.Env["CODEX_HOME"] != "/new" || m.EngineID != "" || m.Env["CUSTOM"] != "member" {
		t.Fatalf("refresh: %+v", m)
	}
	if _, ok := m.Env["REMOVED"]; ok {
		t.Fatal("a variable the profile no longer sets was retained")
	}
	if s.Config.DefaultProfile != "pro" || s.Config.Profiles["pro"].Env["CODEX_HOME"] != "/new" {
		t.Fatalf("the snapshot did not adopt the edited profiles: %+v", s.Config)
	}
	// Resuming again with the same tables changes nothing, so a restart does not
	// keep discarding conversations.
	m.EngineID = "new-session"
	old, current = resumeProfileEnv(s.Config, edited, &m)
	m.applyResumeEnvironment(old, current)
	if m.EngineID != "new-session" {
		t.Fatal("unchanged account cleared")
	}
}

// A member outlives its profile: one added before profiles existed carries no
// profile at all, and a profile can be deleted while the team runs. Neither may
// move an existing member onto a different account.
func TestResumeKeepsMembersWithoutALiveProfile(t *testing.T) {
	saved := config.Config{Profiles: map[string]config.Profile{
		"pro": {Engine: config.Codex, Env: map[string]string{"CODEX_HOME": "/old"}}}}
	for _, m := range []Member{
		{ID: "a", Engine: config.Codex, Profile: "pro", EngineID: "session", Env: map[string]string{"CODEX_HOME": "/old"}},
		{ID: "legacy", Engine: config.Codex, EngineID: "session", Env: map[string]string{"CODEX_HOME": "/old"}},
	} {
		old, current := resumeProfileEnv(&saved, config.Config{}, &m)
		m.applyResumeEnvironment(old, current)
		if m.Env["CODEX_HOME"] != "/old" || m.EngineID != "session" {
			t.Fatalf("%s lost its account when the profile went away: %+v", m.ID, m)
		}
	}
}
