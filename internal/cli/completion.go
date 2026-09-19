package cli

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/ShunL12324/c-squad/internal/squad"
)

func completeResource(kind string) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(cmd *cobra.Command, args []string, prefix string) ([]string, cobra.ShellCompDirective) {
		team, _ := cmd.Flags().GetString("team")
		resource := kind
		if kind == "team-or-member" {
			resource = "member"
		}
		values, err := squad.CompletionValues(team, resource)
		if kind == "team-or-member" {
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
