package squad

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

// reprofile is a member's launch settings re-read from the current
// configuration. The snapshot taken at member add stays the default; this is
// the explicit way to apply an edited profile, such as a changed account
// selector, to a member without losing its identity, tasks or handoff.
type reprofile struct {
	Profile string
	Engine  config.Engine
	Model   string
	Env     map[string]string
	changes []string
	fresh   bool
}

// resolveReprofile reads the named profile, or the member's recorded one, from
// the refreshed snapshot and compares it with what the member runs now.
func resolveReprofile(s *State, m *Member, name string) (*reprofile, error) {
	if name == "" {
		name = m.Profile
	}
	if name == "" {
		return nil, fmt.Errorf("member %s was added without a profile; pass --profile NAME", m.ID)
	}
	cfg, err := s.effectiveConfig()
	if err != nil {
		return nil, err
	}
	p, resolved, err := cfg.ResolveProfile(name, m.ID == "master")
	if err != nil {
		return nil, err
	}
	next := &reprofile{Profile: resolved, Engine: m.Engine, Model: p.Model, Env: profileEnv(p)}
	if p.Engine != "" {
		next.Engine = p.Engine
	}
	next.changes = launchChanges(m, next)
	// A native conversation belongs to one engine and one account directory.
	next.fresh = next.Engine != m.Engine || m.Env[accountSelector(m.Engine)] != next.Env[accountSelector(next.Engine)]
	return next, nil
}

func accountSelector(engine config.Engine) string {
	if engine == config.Codex {
		return "CODEX_HOME"
	}
	return "CLAUDE_CONFIG_DIR"
}

// launchChanges names what differs. Environment values are never included:
// they may hold tokens or account paths.
func launchChanges(m *Member, next *reprofile) []string {
	var out []string
	if m.Profile != next.Profile {
		out = append(out, fmt.Sprintf("profile %q -> %q", m.Profile, next.Profile))
	}
	if m.Engine != next.Engine {
		out = append(out, fmt.Sprintf("engine %s -> %s", m.Engine, next.Engine))
	}
	if m.Model != next.Model {
		out = append(out, fmt.Sprintf("model %q -> %q", m.Model, next.Model))
	}
	var added, removed, changed []string
	for k, v := range next.Env {
		if old, ok := m.Env[k]; !ok {
			added = append(added, k)
		} else if old != v {
			changed = append(changed, k)
		}
	}
	for k := range m.Env {
		if _, ok := next.Env[k]; !ok {
			removed = append(removed, k)
		}
	}
	for _, group := range []struct {
		label string
		keys  []string
	}{{"env added", added}, {"env changed", changed}, {"env removed", removed}} {
		if len(group.keys) > 0 {
			sort.Strings(group.keys)
			out = append(out, group.label+": "+strings.Join(group.keys, ", "))
		}
	}
	return out
}

func (r *reprofile) apply(s *State, actor string, m *Member) {
	m.Profile, m.Engine, m.Model, m.Env = r.Profile, r.Engine, r.Model, agentenv.Merge(r.Env)
	if r.fresh {
		m.EngineID = ""
	}
	s.event(actor, "reprofiled", r.summary(m.ID))
}

func (r *reprofile) summary(id string) string {
	changes := "no changes"
	if len(r.changes) > 0 {
		changes = strings.Join(r.changes, "; ")
	}
	conversation := "conversation resumes"
	if r.fresh {
		conversation = "engine or account changed, conversation starts fresh"
	}
	return fmt.Sprintf("Reprofile %s: %s; %s", id, changes, conversation)
}

// profileDrift reports whether the current configuration would launch a member
// differently from what resume will launch, so resume can say how to apply the
// rest of the edit. m is the member after the resume environment update, and
// saved is its profile's environment as the team last read it: a variable the
// profile dropped but the member still carries is a difference too.
func profileDrift(cfg config.Config, m *Member, saved map[string]string) bool {
	p, ok := cfg.Profiles[m.Profile]
	if !ok {
		return false
	}
	if p.Engine != "" && p.Engine != m.Engine || p.Model != m.Model {
		return true
	}
	for k, v := range p.Env {
		if m.Env[k] != v {
			return true
		}
	}
	for k := range saved {
		if _, kept := p.Env[k]; !kept {
			if _, carried := m.Env[k]; carried {
				return true
			}
		}
	}
	return false
}

// profileDriftNotice is the one line resume prints for drifted members.
func profileDriftNotice(names []string) string {
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return "Profile notice: the current profile of " + strings.Join(names, ", ") + " differs from what resume launches (engine and model stay as added; only variables inherited from the profile follow its edits). Apply the whole profile with csquad member restart NAME --reprofile, or csquad recover --reprofile for Master."
}
