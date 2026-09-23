package squad

import (
	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

// resetRuntime clears only transient identities after the previous process tree
// has stopped. Generation fencing and native conversation identity are separate.
func (m *Member) resetRuntime() {
	m.State = MemberStateStarting
	m.Peer = ""
	m.Pane = ""
	m.RunnerPID = 0
	m.EnginePID = 0
	m.ProcessStart = ""
	m.Processes = nil
	m.ObservedAt = ""
}

func (m *Message) resetDelivery() {
	if m.State == DeliveryStateSuperseded {
		return
	}
	m.State = DeliveryStatePending
	m.Attempt = ""
	m.Attempts = 0
	m.Error = ""
}

// recoverDelivery resets failed or interrupted transport, never successful
// delivery merely because the recipient did not send an optional ACK.
func (m *Message) recoverDelivery() {
	if legacyBrief(m) {
		return
	}
	switch m.State {
	case DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded:
		return
	default:
		m.resetDelivery()
	}
}

// applyResumeEnvironment moves a member onto its profile's current environment.
// Values the member still holds from the profile it was added with follow the
// edit, and a variable the profile no longer sets is dropped; anything else the
// member carries is left alone, because a resume must not rewrite state the
// member acquired elsewhere.
func (m *Member) applyResumeEnvironment(oldDefaults, currentDefaults map[string]string) {
	selector := "CLAUDE_CONFIG_DIR"
	if m.Engine == config.Codex {
		selector = "CODEX_HOME"
	}
	before := m.Env[selector]
	changes := map[string]string{}
	for key, value := range currentDefaults {
		old, inherited := oldDefaults[key]
		existing, present := m.Env[key]
		if inherited && existing == old || !inherited && !present {
			changes[key] = value
		}
	}
	for key, old := range oldDefaults {
		if _, exists := currentDefaults[key]; !exists && m.Env[key] == old {
			delete(m.Env, key)
		}
	}
	m.Env = agentenv.Merge(m.Env, changes)
	if before != m.Env[selector] {
		m.EngineID = ""
	}
}

// resumeProfileEnv reports the environment a member's profile supplied when the
// team was saved and what the same profile supplies now, so a resume adopts an
// edited profile. A profile that has since been deleted or switched to another
// engine, like a member added before profiles existed, reports the saved values
// unchanged: a member keeps the account it was launched with instead of moving
// mid-team onto one configured for a different engine.
func resumeProfileEnv(saved *config.Config, current config.Config, m *Member) (map[string]string, map[string]string) {
	old := map[string]string{}
	if saved != nil {
		old = agentenv.Merge(saved.Profiles[m.Profile].Env)
	}
	if p, ok := current.Profiles[m.Profile]; !ok || !p.Launches(m.Engine) {
		return old, old
	}
	return old, agentenv.Merge(current.Profiles[m.Profile].Env)
}
