package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/ShunL12324/c-squad/internal/preflight"
	"github.com/ShunL12324/c-squad/internal/squad"
)

type runner func([]string, map[string]string, []string) error

type usageError struct {
	cause   error
	command string
}

func (e *usageError) Error() string { return e.cause.Error() }
func (e *usageError) Unwrap() error { return e.cause }

// Run parses one invocation. Help and completion never execute a team command.
func Run(args []string) error {
	root := newCommand(squad.Execute)
	root.SetArgs(args)
	cmd, err := root.ExecuteC()
	if err == nil {
		return nil
	}
	var usage *usageError
	if errors.As(err, &usage) {
		return err
	}
	if cmd != nil && cmd.Annotations["executed"] == "true" {
		return err
	}
	name := "csquad"
	if cmd != nil {
		name = cmd.CommandPath()
	}
	return &usageError{cause: err, command: name}
}

// Report writes a failure once to stderr and returns its process exit code.
// Reporting is best-effort: a broken stderr must not hide the original exit code.
// Usage failures exit 2; operational failures exit 1 and retain their cause text.
func Report(w io.Writer, err error) int {
	_, _ = fmt.Fprintln(w, "csquad:", err)
	var usage *usageError
	if errors.As(err, &usage) {
		_, _ = fmt.Fprintf(w, "Run '%s --help' for usage and examples.\n", usage.command)
		return 2
	}
	switch {
	case errors.Is(err, preflight.ErrMissingDependency):
		_, _ = fmt.Fprintln(w, "Run 'csquad doctor' for the full environment report. Git is required only for code tasks.")
	case errors.Is(err, squad.ErrStaleGeneration):
		_, _ = fmt.Fprintln(w, "This session is outdated. Use the current member session or recover from an outside terminal.")
	case errors.Is(err, squad.ErrTeamStopped):
		_, _ = fmt.Fprintln(w, "Run 'csquad resume --help' to recover the team.")
	case errors.Is(err, squad.ErrMemberLimit):
		_, _ = fmt.Fprintln(w, "Remove an unused member or adjust max_members before starting a new team.")
	case errors.Is(err, squad.ErrNotFound):
		_, _ = fmt.Fprintln(w, "Run 'csquad board' to inspect current resource IDs.")
	}
	return 1
}

func newCommand(run runner) *cobra.Command {
	root := &cobra.Command{Use: "csquad", Short: "Coordinate Claude Code and Codex teams in tmux", Long: "Start a master session, delegate tasks, and recover teams in the current project.\nWith no command, csquad starts a new team. Use --help on any command for details.\nUse 'help request' to ask master a question; '--help' displays CLI usage.", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, Example: "  csquad start --name my-team\n  csquad member add reviewer --engine claude --role reviewer\n  csquad board\n  csquad resume --name my-team"}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.PersistentFlags().String("team", "", "Team state directory (defaults to the current project's last team)")
	root.PersistentFlags().String("member", "", "Calling member identity (injected for agents)")
	root.PersistentFlags().Int("generation", 0, "Session generation for stale-write protection; not a turn limit")
	if err := root.MarkPersistentFlagDirname("team"); err != nil {
		panic(err)
	}
	if err := root.RegisterFlagCompletionFunc("member", completeResource("member")); err != nil {
		panic(err)
	}
	if err := root.RegisterFlagCompletionFunc("generation", cobra.NoFileCompletions); err != nil {
		panic(err)
	}
	root.RunE = execute(run, []string{"start"})
	addFlags(root, startupFlags)
	for _, def := range definitions() {
		parent := root
		parts := strings.Fields(def.path)
		for _, name := range parts[:len(parts)-1] {
			var next *cobra.Command
			for _, child := range parent.Commands() {
				if child.Name() == name {
					next = child
					break
				}
			}
			if next == nil {
				next = &cobra.Command{Use: name, Short: groupDescription(name), Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
				parent.AddCommand(next)
			}
			parent = next
		}
		cmd := &cobra.Command{Use: parts[len(parts)-1] + def.args, Short: def.summary, Example: def.example, Hidden: def.hidden, Args: cobra.RangeArgs(def.min, def.max), RunE: execute(run, parts), ValidArgsFunction: cobra.NoFileCompletions}
		if def.max < 0 {
			cmd.Args = cobra.MinimumNArgs(def.min)
		}
		addFlags(cmd, def.flags)
		for _, name := range def.required {
			if err := cmd.MarkFlagRequired(name); err != nil {
				panic(err)
			}
		}
		if def.path == "message broadcast" {
			cmd.MarkFlagsOneRequired("task", "all")
			cmd.MarkFlagsMutuallyExclusive("task", "all")
		}
		if def.complete != "" {
			cmd.ValidArgsFunction = completePositional(def.complete)
		}
		parent.AddCommand(cmd)
	}
	root.AddCommand(&cobra.Command{Use: "help-cli", Hidden: true, Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return root.Help() }})
	// The existing help command is an escalation workflow, not Cobra's help alias.
	root.SetHelpCommand(&cobra.Command{Use: "usage [command...]", Short: "Show help for a command", RunE: func(c *cobra.Command, args []string) error {
		target, rest, err := root.Find(args)
		if err != nil {
			return err
		}
		if len(rest) > 0 {
			return fmt.Errorf("unknown help topic %q", strings.Join(rest, " "))
		}
		return target.Help()
	}})
	return root
}

func execute(run runner, path []string) func(*cobra.Command, []string) error {
	return func(cmd *cobra.Command, args []string) error {
		values := map[string]string{}
		cmd.Flags().Visit(func(flag *pflag.Flag) {
			if flag.Name == "help" {
				return
			}
			if flag.Name == "env" {
				v, _ := cmd.Flags().GetStringArray("env")
				values[flag.Name] = strings.Join(v, "\x00")
			} else {
				values[flag.Name] = flag.Value.String()
			}
		})
		for _, name := range []string{"generation", "epoch", "expected-generation"} {
			if value, ok := values[name]; ok {
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					return &usageError{fmt.Errorf("--%s must be a nonnegative integer", name), cmd.CommandPath()}
				}
			}
		}
		for name, choices := range flagChoices {
			if value, ok := values[name]; ok && !contains(choices, value) {
				return &usageError{fmt.Errorf("invalid --%s %q; choose %s", name, value, strings.Join(choices, "|")), cmd.CommandPath()}
			}
		}
		for _, name := range []string{"text", "acceptance", "summary", "sha", "owner"} {
			if v, ok := values[name]; ok && strings.TrimSpace(v) == "" {
				return &usageError{fmt.Errorf("--%s must not be empty", name), cmd.CommandPath()}
			}
		}
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations["executed"] = "true"
		if path[0] == "run-engine" {
			return run(path, values, args)
		}
		return run(append(append([]string(nil), path...), args...), values, nil)
	}
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
