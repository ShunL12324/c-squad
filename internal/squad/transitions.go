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
	m.State = DeliveryStatePending
	m.Attempt = ""
	m.Attempts = 0
	m.Error = ""
	m.RecipientGeneration = 0
}

// applyResumeEnvironment updates inherited defaults while retaining per-member
// overrides. Native conversation IDs belong to the selected configuration home.
func (m *Member) applyResumeEnvironment(oldDefaults, currentDefaults, explicit map[string]string) {
	selector := "CLAUDE_CONFIG_DIR"
	if m.Engine == config.Codex {
		selector = "CODEX_HOME"
	}
	before := m.Env[selector]
	changes := map[string]string{}
	for key, value := range currentDefaults {
		if old, ok := oldDefaults[key]; !ok || m.Env[key] == old {
			changes[key] = value
		}
	}
	m.Env = agentenv.Merge(m.Env, changes, explicit)
	if before != m.Env[selector] {
		m.EngineID = ""
	}
}
