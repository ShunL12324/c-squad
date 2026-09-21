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
	m.RecipientGeneration = 0
}

// recoverDelivery resets failed or interrupted transport, never successful
// delivery merely because the recipient did not send an optional ACK.
func (m *Message) recoverDelivery() {
	m.migrateLegacyACKWarning()
	switch m.State {
	case DeliveryStateSent, DeliveryStateAcknowledged, DeliveryStateSuperseded:
		return
	default:
		m.resetDelivery()
	}
}

// These exact diagnostics were emitted only after successful transport in older
// versions. Preserve their audit text without presenting them as delivery errors.
// Unknown needs_attention causes remain untouched.
func (m *Message) migrateLegacyACKWarning() {
	if m.State != DeliveryStateNeedsAttention || m.Attempts < 1 {
		return
	}
	switch m.Error {
	case "Transport succeeded but no agent ACK; inspect inbox/recipient or explicitly message retry (no automatic reinjection)",
		"No agent ACK after 3 delivery attempts; inspect recipient or restart/requeue":
		m.DeliveryNote = "Legacy optional-ACK warning cleared: " + m.Error
		m.State = DeliveryStateSent
		m.Error = ""
	}
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
		if m.EnvOverrides != nil {
			if _, explicit := (*m.EnvOverrides)[key]; explicit {
				continue
			}
		}
		old, inherited := oldDefaults[key]
		existing, present := m.Env[key]
		if inherited && existing == old || !inherited && !present {
			changes[key] = value
		}
	}
	for key, old := range oldDefaults {
		if m.EnvOverrides != nil {
			if _, explicit := (*m.EnvOverrides)[key]; explicit {
				continue
			}
		}
		if _, exists := currentDefaults[key]; !exists && m.Env[key] == old {
			delete(m.Env, key)
		}
	}
	if m.EnvOverrides != nil {
		m.EnvOverrides = explicitMemberEnv(agentenv.Merge(*m.EnvOverrides, explicit))
		changes = agentenv.Merge(changes, *m.EnvOverrides)
	}
	m.Env = agentenv.Merge(m.Env, changes, explicit)
	if before != m.Env[selector] {
		m.EngineID = ""
	}
}

// Reload both supported config tables; saved startup values must not hide edits
// to [startup_env]. New teams distinguish CLI overrides from config defaults.
func refreshResumeDefaults(s *State, current config.Config, overrides map[string]string) (map[string]string, map[string]string) {
	old := map[string]string{}
	startup := map[string]string{}
	if s.Config != nil {
		old = agentenv.Merge(s.Config.Env, s.Config.StartupEnv)
		if s.StartupOverrides == nil {
			// Legacy snapshots mixed config and CLI values. Preserve unknown overrides,
			// but current explicit config entries take priority over that old snapshot.
			startup = agentenv.Merge(s.Config.StartupEnv)
			for key := range agentenv.Merge(current.Env, current.StartupEnv) {
				delete(startup, key)
			}
		}
	}
	if s.StartupOverrides != nil {
		startup = agentenv.Merge(*s.StartupOverrides)
	}
	startup = agentenv.Merge(startup, overrides)
	s.StartupOverrides = &startup
	env := agentenv.Merge(agentenv.SnapshotSelectors(), current.Env)
	startupEnv := agentenv.Merge(current.StartupEnv, startup)
	if s.Config == nil {
		s.Config = &current
	}
	s.Config.Env, s.Config.StartupEnv = env, startupEnv
	// A full resume adopts edited command paths, just like account environment
	// defaults. Individual member restarts continue using the saved snapshot.
	s.Config.EngineCommands = current.EngineCommands
	return old, agentenv.Merge(env, startupEnv)
}
