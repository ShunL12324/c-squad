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
	root := &cobra.Command{Use: "csquad", Short: "Coordinate Claude Code and Codex teams in tmux", Long: "Start a master session, delegate tasks, and recover teams in the current project.\nWith no command, csquad starts a new team. Use --help on any command for details.\nUse 'question request' to ask master a question; '--help' displays CLI usage.", SilenceUsage: true, SilenceErrors: true, Args: cobra.NoArgs, Example: "  csquad start my-team\n  csquad member add reviewer --engine claude --role reviewer\n  csquad board\n  csquad resume my-team"}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.PersistentFlags().String("team", "", "Team state directory (defaults to the current project's last team)")
	root.PersistentFlags().String("state-dir", "", "Team state directory (alias of --team)")
	root.PersistentFlags().String("team-name", "", "Select an existing team by exact name")
	_ = root.MarkPersistentFlagDirname("state-dir")
	_ = root.RegisterFlagCompletionFunc("team-name", completeResource("team"))
	root.PersistentFlags().String("member", "", "Calling member identity; inside a member session identity is bound and a conflicting value is refused")
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
				next = &cobra.Command{Use: name, Hidden: name == "_internal", Short: groupDescription(name), Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error { return c.Help() }}
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
			if fileInput(name) {
				cmd.MarkFlagsOneRequired(name, name+"-file")
				continue
			}
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
	// Owning 'completion' replaces Cobra's default command, whose help documents
	// persistence through Homebrew only.
	root.AddCommand(completionCommand(root))
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
		operation := append([]string(nil), path...)
		if operation[0] == "_internal" {
			operation = operation[1:]
		}
		if operation[0] == "question" {
			operation[0] = "help"
		}
		if len(operation) == 2 && operation[0] == "message" && operation[1] == "reply" {
			operation = []string{"reply"}
		}
		if err := normalizeInputs(cmd, operation, values, &args); err != nil {
			return &usageError{err, cmd.CommandPath()}
		}
		for _, name := range []string{"generation", "epoch", "expected-generation"} {
			if value, ok := values[name]; ok {
				n, err := strconv.Atoi(value)
				if err != nil || n < 0 {
					return &usageError{fmt.Errorf("--%s must be a nonnegative integer", name), cmd.CommandPath()}
				}
			}
		}
		for name, choices := range flagChoices {
			if operation[0] == "ui-panel" && name == "view" && values[name] == "header" {
				continue
			}
			if value, ok := values[name]; ok && !contains(choices, value) {
				return &usageError{fmt.Errorf("invalid --%s %q; choose %s", name, value, strings.Join(choices, "|")), cmd.CommandPath()}
			}
		}
		for _, name := range []string{"text", "acceptance", "summary", "sha", "submission", "owner", "reason", "repo"} {
			if v, ok := values[name]; ok && strings.TrimSpace(v) == "" {
				return &usageError{fmt.Errorf("--%s must not be empty", name), cmd.CommandPath()}
			}
		}
		if cmd.Annotations == nil {
			cmd.Annotations = map[string]string{}
		}
		cmd.Annotations["executed"] = "true"
		if operation[0] == "run-engine" {
			return run(operation, values, args)
		}
		return run(append(append([]string(nil), operation...), args...), values, nil)
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

// Normalize aliases before dispatch so authorization sees the original operation.
func normalizeInputs(cmd *cobra.Command, path []string, values map[string]string, args *[]string) error {
	selectors := []string{"team", "state-dir", "team-name"}
	if contains([]string{"start", "resume", "stop", "recover", "attach", "board", "ui"}, path[0]) {
		selectors = append(selectors, "name")
	}
	count := 0
	for _, key := range selectors {
		if value, ok := values[key]; ok {
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("--%s must not be empty", key)
			}
			count++
		}
	}
	if (contains([]string{"start", "resume", "stop", "recover"}, path[0]) || (len(path) == 2 && path[0] == "team" && path[1] == "remove")) && len(*args) == 1 {
		count++
		values["name"] = (*args)[0]
		*args = nil
	}
	if count > 1 {
		return fmt.Errorf("specify the team once: positional name, --name, --team-name, --state-dir, or --team")
	}
	if path[0] == "start" && (values["team"] != "" || values["state-dir"] != "" || values["team-name"] != "") {
		return fmt.Errorf("start creates a new team; use start NAME or --name NAME")
	}
	if value, ok := values["state-dir"]; ok {
		values["team"] = value
		delete(values, "state-dir")
	}
	stdinCount := 0
	for _, key := range []string{"text", "summary", "instructions", "description"} {
		if filename, ok := values[key+"-file"]; ok {
			if _, inline := values[key]; inline {
				return fmt.Errorf("--%s and --%s-file are mutually exclusive", key, key)
			}
			if filename == "" {
				return fmt.Errorf("--%s-file requires a filename or '-'", key)
			}
			if filename == "-" {
				stdinCount++
			}
		}
	}
	if stdinCount > 1 {
		return fmt.Errorf("only one input field may read stdin")
	}
	for _, key := range []string{"text", "summary", "instructions", "description"} {
		if filename, ok := values[key+"-file"]; ok {
			var data []byte
			var err error
			if filename == "-" {
				data, err = io.ReadAll(cmd.InOrStdin())
			} else {
				data, err = os.ReadFile(filename)
			}
			if err != nil {
				return fmt.Errorf("read --%s-file: %w", key, err)
			}
			if strings.TrimSpace(string(data)) == "" {
				return fmt.Errorf("--%s-file must not be empty", key)
			}
			values[key] = string(data)
			delete(values, key+"-file")
		}
	}
	return nil
}
