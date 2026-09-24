package cli

import (
	"os"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ShunL12324/c-squad/internal/squad"
)

func completeResource(kind string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		selection := map[string]string{}
		for _, key := range []string{"team", "state-dir", "team-name"} {
			if cmd.Flags().Changed(key) {
				selection[key], _ = cmd.Flags().GetString(key)
			}
		}
		if slices.Contains([]string{"attach", "resume", "stop", "recover", "repin", "board", "ui"}, cmd.Name()) && cmd.Flags().Changed("name") {
			selection["name"], _ = cmd.Flags().GetString("name")
		}
		argsForSelection := []string{}
		if err := normalizeInputs(cmd, []string{cmd.Name()}, selection, &argsForSelection); err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		team, name := selection["team"], selection["team-name"]
		if selection["name"] != "" {
			name = selection["name"]
		}
		resource := kind
		if kind == "team-or-member" {
			resource = "member"
		}
		if resource != "team" {
			resolved, err := squad.ResolveTeamDirectory(team, name)
			if err != nil {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			team = resolved
		}
		values, err := squad.CompletionValues(team, resource)
		if kind == "team-or-member" && len(selection) == 0 && os.Getenv("CSQUAD_STATE_DIR") == "" {
			names, e := squad.CompletionValues("", "team")
			if e == nil {
				values = append(values, names...)
				err = nil
			}
		}
		if err != nil {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		// Comma-separated participant/dependency flags keep their already-entered prefix.
		head := ""
		if at := strings.LastIndex(prefix, ","); at >= 0 {
			head, prefix = prefix[:at+1], prefix[at+1:]
		}
		out := []string{}
		for _, value := range values {
			if strings.HasPrefix(value, prefix) {
				out = append(out, head+value)
			}
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

func completePositional(kind string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	complete := completeResource(kind)
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return complete(cmd, args, prefix)
	}
}
