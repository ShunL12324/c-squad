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
			return removedLaunchFlagError(flag)
		}
	}
	return nil
}

// RejectRemovedLaunchFlags reports the first removed launch flag present on a
// command line, even with an empty value. The parser calls it before anything
// else, so an unrelated failure such as "no team selected" or a value check
// cannot hide the explanation. doctor still takes --engine and is exempt.
func RejectRemovedLaunchFlags(operation string, set map[string]string) error {
	for _, flag := range removedLaunchFlags {
		if operation == "doctor" && flag == "engine" {
			continue
		}
		if _, ok := set[flag]; ok {
			if operation == "resume" && (flag == "engine" || flag == "model") {
				// resume has no --profile to point at: members keep the launch
				// settings they were added with, and Master's come from its profile.
				return fmt.Errorf("--%s was removed; set %s in a [profiles.NAME] table and select it with start --profile or member add --profile", flag, flag)
			}
			return removedLaunchFlagError(flag)
		}
	}
	return nil
}

func removedLaunchFlagError(flag string) error {
	return fmt.Errorf("--%s was removed; %s", flag, removedLaunchFlagHelp[flag])
}

// profileEnv layers the environment a member launches with: the inherited
// selectors, then the profile. A profile is the only source of launch settings,
// so there is no command-line or shared table above it.
func profileEnv(p config.Profile) map[string]string {
	return agentenv.Merge(agentenv.SnapshotSelectors(), p.Env)
}
