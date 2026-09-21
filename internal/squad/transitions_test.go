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
		msg.State, msg.Error, msg.Attempt = DeliveryStateNeedsAttention, "old transport error", "old-attempt"
		msg.Attempts, msg.RecipientGeneration = 3, m.Generation
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
	if msg.State != DeliveryStatePending || msg.Error != "" || msg.Attempts != 0 || msg.Attempt != "" || msg.RecipientGeneration != 0 || msg.Text != "continue review" {
		t.Fatal("restart did not preserve message content and reset its delivery lease")
	}
}

func TestResumeEnvironmentUsesUpdatedDefaults(t *testing.T) {
	m := Member{Engine: "codex", EngineID: "old-thread", Env: map[string]string{"CODEX_HOME": "/old", "CUSTOM": "member"}}
	m.applyResumeEnvironment(map[string]string{"CODEX_HOME": "/old", "CUSTOM": "global"}, map[string]string{"CODEX_HOME": "/new", "CUSTOM": "new-global"}, nil)
	if m.Env["CODEX_HOME"] != "/new" || m.Env["CUSTOM"] != "member" || m.EngineID != "" {
		t.Fatalf("incorrect recovery environment: %+v", m)
	}
	m.EngineID = "new-thread"
	m.applyResumeEnvironment(map[string]string{"CODEX_HOME": "/new"}, map[string]string{"CODEX_HOME": "/new"}, nil)
	if m.EngineID != "new-thread" {
		t.Fatal("unchanged account lost its conversation")
	}
	m.applyResumeEnvironment(nil, nil, map[string]string{"CODEX_HOME": "/explicit"})
	if m.Env["CODEX_HOME"] != "/explicit" || m.EngineID != "" {
		t.Fatal("explicit account override was not applied")
	}
}

func TestResumeRefreshesStartupConfiguration(t *testing.T) {
	for _, modern := range []bool{false, true} {
		s := &State{Config: &config.Config{Env: map[string]string{"REMOVED": "old"}, StartupEnv: map[string]string{"CODEX_HOME": "/old"}}}
		if modern {
			empty := map[string]string{}
			s.StartupOverrides = &empty
		}
		m := Member{Engine: config.Codex, EngineID: "old-session", Env: map[string]string{"CODEX_HOME": "/old", "REMOVED": "old", "CUSTOM": "member"}}
		old, current := refreshResumeDefaults(s, config.Config{StartupEnv: map[string]string{"CODEX_HOME": "/new"}}, nil)
		m.applyResumeEnvironment(old, current, nil)
		if m.Env["CODEX_HOME"] != "/new" || m.EngineID != "" || m.Env["CUSTOM"] != "member" {
			t.Fatalf("refresh: %+v", m)
		}
		if _, ok := m.Env["REMOVED"]; ok {
			t.Fatal("deleted config retained")
		}
		m.EngineID = "new-session"
		old, current = refreshResumeDefaults(s, config.Config{StartupEnv: map[string]string{"CODEX_HOME": "/new"}}, nil)
		m.applyResumeEnvironment(old, current, nil)
		if m.EngineID != "new-session" {
			t.Fatal("unchanged account cleared")
		}
	}
}

func TestResumeRetainsExplicitStartupOverride(t *testing.T) {
	overrides := map[string]string{"CODEX_HOME": "/explicit"}
	s := &State{StartupOverrides: &overrides, Config: &config.Config{StartupEnv: overrides}}
	_, current := refreshResumeDefaults(s, config.Config{Env: map[string]string{"CODEX_HOME": "/configured"}}, nil)
	if current["CODEX_HOME"] != "/explicit" {
		t.Fatal("lost explicit startup override")
	}
	_, current = refreshResumeDefaults(s, config.Config{}, map[string]string{"CODEX_HOME": "/resume"})
	if current["CODEX_HOME"] != "/resume" {
		t.Fatal("resume override ignored")
	}
}

func TestResumePreservesMemberOverridesEvenWhenEqualToOldDefault(t *testing.T) {
	for _, value := range []string{"/old", "/member"} {
		m := Member{Engine: config.Codex, Env: map[string]string{"CODEX_HOME": value, "CUSTOM": "member"}, EnvOverrides: explicitMemberEnv(map[string]string{"CODEX_HOME": value, "CUSTOM": "member"})}
		m.applyResumeEnvironment(map[string]string{"CODEX_HOME": "/old"}, map[string]string{"CODEX_HOME": "/new", "CUSTOM": "new-default"}, nil)
		if m.Env["CODEX_HOME"] != value || m.Env["CUSTOM"] != "member" {
			t.Fatalf("member override lost: %+v", m.Env)
		}
		m.applyResumeEnvironment(nil, nil, map[string]string{"CODEX_HOME": "/resume"})
		m.applyResumeEnvironment(nil, map[string]string{"CODEX_HOME": "/later"}, nil)
		if m.Env["CODEX_HOME"] != "/resume" {
			t.Fatal("resume override was not retained")
		}
	}
}

func TestLegacyStartupMovedToEnvUsesCurrentAccount(t *testing.T) {
	s := &State{Config: &config.Config{StartupEnv: map[string]string{"CODEX_HOME": "/old"}}}
	m := Member{Engine: config.Codex, EngineID: "old-session", Env: map[string]string{"CODEX_HOME": "/old"}}
	old, current := refreshResumeDefaults(s, config.Config{Env: map[string]string{"CODEX_HOME": "/new"}}, nil)
	m.applyResumeEnvironment(old, current, nil)
	if m.Env["CODEX_HOME"] != "/new" || m.EngineID != "" {
		t.Fatalf("old startup masked current config: %+v", m)
	}
}
