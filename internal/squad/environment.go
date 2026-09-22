package squad

import (
	"fmt"

	"github.com/ShunL12324/c-squad/internal/agentenv"
	"github.com/ShunL12324/c-squad/internal/config"
)

// removedLaunchFlags named launch settings that a profile now owns. They stay
// registered so an existing script fails with this message instead of a generic
// unknown-flag error, and so nothing launches with settings the command line
// asked for but no profile carries.
var removedLaunchFlags = []string{"engine", "model", "env", "role", "template"}

var removedLaunchFlagHelp = map[string]string{
	"engine":   "set engine in a [profiles.NAME] table and select it with --profile",
	"model":    "set model in a [profiles.NAME] table and select it with --profile",
	"env":      "set the variables in that profile's [profiles.NAME.env] table",
	"role":     "the member name is the label; describe the work with --instructions",
	"template": "select launch settings with --profile and supply responsibilities with --instructions",
}

func rejectRemovedLaunchFlags(o options) error {
	for _, flag := range removedLaunchFlags {
		if o[flag] != "" {
			return fmt.Errorf("--%s was removed; %s", flag, removedLaunchFlagHelp[flag])
		}
	}
	return nil
}

// profileEnv layers the environment a member launches with: the inherited
// selectors, then the profile. A profile is the only source of launch settings,
// so there is no command-line or shared table above it.
func profileEnv(p config.Profile) map[string]string {
	return agentenv.Merge(agentenv.SnapshotSelectors(), p.Env)
}
