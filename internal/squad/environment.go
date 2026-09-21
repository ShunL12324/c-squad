package squad

import (
	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

func memberEnv(cfg config.Config, t config.Template, overrides map[string]string) map[string]string {
	return agentenv.Merge(agentenv.SnapshotSelectors(), cfg.Env, t.Env, cfg.StartupEnv, overrides)
}

func explicitMemberEnv(overrides map[string]string) *map[string]string {
	values := agentenv.Merge(overrides)
	return &values
}
