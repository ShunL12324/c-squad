package cli

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/ShunL12324/c-squad/internal/queue"
)

type usageError struct {
	cause   error
	command string
}

func (e *usageError) Error() string { return e.cause.Error() }
func (e *usageError) Unwrap() error { return e.cause }

// Run parses and executes one invocation.
func Run(args []string) error {
	root := newCommand()
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
// Usage failures exit 2; operational failures exit 1.
func Report(w io.Writer, err error) int {
	_, _ = fmt.Fprintln(w, "csquad:", err)
	var usage *usageError
	if errors.As(err, &usage) {
		_, _ = fmt.Fprintf(w, "Run '%s --help' for usage and examples.\n", usage.command)
		return 2
	}
	if errors.Is(err, queue.ErrNotFound) {
		_, _ = fmt.Fprintln(w, "Run 'csquad ls --all' to see task IDs.")
	}
	return 1
}

func newCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "csquad [-- CLAUDE_ARGS...]",
		Short: "Queue tasks as Claude Code background sessions",
		Long: "csquad queues tasks and runs each as a Claude Code background session, a few\n" +
			"at a time. Watch and answer the sessions in Claude Code's agent view\n" +
			"('claude agents'); use 'csquad ls' and 'csquad peek' for a quick look.\n\n" +
			"With no command, csquad starts an interactive Claude Code session with the\n" +
			"csquad console prompt appended. Arguments after -- go to claude.",
		Example: "  csquad\n" +
			"  csquad -- --model opus\n" +
			"  csquad add 'Add a --done filter to the list command and test it'\n" +
			"  csquad ls\n" +
			"  csquad peek T3",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args: func(c *cobra.Command, args []string) error {
			if len(args) > 0 && c.ArgsLenAtDash() != 0 {
				return fmt.Errorf("unknown command %q; pass claude arguments after --", args[0])
			}
			return nil
		},
		RunE: func(c *cobra.Command, args []string) error {
			markExecuted(c)
			return console(args)
		},
	}
	root.SetOut(os.Stdout)
	root.SetErr(os.Stderr)
	root.AddCommand(addCommand(), lsCommand(), peekCommand(), cancelCommand(), dispatchCommand(),
		doctorCommand(), configCommand(), versionCommand(), updateCommand(), completionCommand(root))
	root.CompletionOptions.DisableDefaultCmd = true
	return root
}

// markExecuted keeps failures after this point out of the usage-error path.
func markExecuted(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["executed"] = "true"
}
