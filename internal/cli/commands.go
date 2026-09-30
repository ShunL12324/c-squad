package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
	"github.com/ShunL12324/c-squad/internal/claude"
	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/dispatch"
	"github.com/ShunL12324/c-squad/internal/paths"
	"github.com/ShunL12324/c-squad/internal/prompts"
	"github.com/ShunL12324/c-squad/internal/queue"
	"github.com/ShunL12324/c-squad/internal/update"
)

// pollInterval is how often the dispatcher checks for free slots.
const pollInterval = 5 * time.Second

// console replaces this process with an interactive claude session that has
// the console prompt appended.
func console(args []string) error {
	bin, err := claude.Binary()
	if err != nil {
		return err
	}
	argv := append([]string{"claude", "--append-system-prompt", prompts.Console}, args...)
	return syscall.Exec(bin, argv, os.Environ())
}

func openStore() (*queue.Store, string, error) {
	dir, err := paths.Data()
	if err != nil {
		return nil, "", err
	}
	store, err := queue.Open(filepath.Join(dir, "csquad.db"))
	return store, dir, err
}

func addCommand() *cobra.Command {
	var cwd, agent, name, model, file string
	cmd := &cobra.Command{
		Use:   "add [flags] [--] PROMPT...",
		Short: "Queue a task to run as a background session",
		Long: "Queue a task. The dispatcher starts it as a Claude Code background session in\n" +
			"--cwd once a slot is free, and is started automatically when not running.\n" +
			"The prompt is everything the session will know, so make it self-contained.",
		Example: "  csquad add 'Fix the flaky login test'\n" +
			"  csquad add --cwd ~/src/api --agent reviewer -- 'Review the auth branch'\n" +
			"  csquad add --file task.md",
		RunE: func(c *cobra.Command, args []string) error {
			prompt := strings.TrimSpace(strings.Join(args, " "))
			if file != "" {
				if prompt != "" {
					return errors.New("give the prompt as arguments or with --file, not both")
				}
				data, err := readInput(c.InOrStdin(), file)
				if err != nil {
					return err
				}
				prompt = strings.TrimSpace(string(data))
			}
			if prompt == "" {
				return errors.New("the task prompt is empty")
			}
			markExecuted(c)
			dir, err := paths.Canonical(cwd)
			if err != nil {
				return fmt.Errorf("task directory: %w", err)
			}
			if info, err := os.Stat(dir); err != nil || !info.IsDir() {
				return fmt.Errorf("task directory %s is not a directory", dir)
			}
			store, dataDir, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			t, err := store.Add(queue.Task{Prompt: prompt, Name: name, Cwd: dir, Agent: agent, Model: model})
			if err != nil {
				return err
			}
			pos, err := store.Position(t.ID)
			if err != nil {
				return err
			}
			_, _ = fmt.Fprintf(c.OutOrStdout(), "%s queued (position %d) in %s\n", t.Label(), pos, dir)
			exe, err := os.Executable()
			if err != nil {
				return err
			}
			if err := dispatch.Start(exe, dataDir); err != nil {
				return fmt.Errorf("%s is queued, but %w; run 'csquad dispatch'", t.Label(), err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&cwd, "cwd", ".", "Directory the session starts in")
	cmd.Flags().StringVar(&agent, "agent", "", "Claude Code agent to run as (see .claude/agents)")
	cmd.Flags().StringVar(&name, "name", "", "Session display name (default: task ID and prompt summary)")
	cmd.Flags().StringVar(&model, "model", "", "Model for this session (default: config model)")
	cmd.Flags().StringVar(&file, "file", "", "Read the prompt from FILE, or '-' for stdin")
	_ = cmd.MarkFlagDirname("cwd")
	return cmd
}

func readInput(stdin io.Reader, file string) ([]byte, error) {
	if file == "-" {
		return io.ReadAll(stdin)
	}
	return os.ReadFile(file)
}

func lsCommand() *cobra.Command {
	var all bool
	var limit int
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List tasks with live session state",
		Long: "List tasks queued from this directory or below (--all for every project),\n" +
			"with the live state of their sessions and each session's latest message.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			store, dataDir, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			tasks, err := store.List("")
			if err != nil {
				return err
			}
			here, err := paths.Canonical(".")
			if err != nil {
				return err
			}
			if !all {
				tasks = within(tasks, here)
			}
			if limit > 0 && len(tasks) > limit {
				tasks = tasks[len(tasks)-limit:]
			}
			sessions, sessErr := claude.Sessions(c.Context())
			if sessErr != nil {
				_, _ = fmt.Fprintln(c.ErrOrStderr(), "csquad: session state unavailable:", sessErr)
			}
			now := time.Now()
			w := tabwriter.NewWriter(c.OutOrStdout(), 0, 0, 2, ' ', 0)
			header := "ID\tSTATE\tAGE\tSESSION\tTASK\tLATEST"
			if all {
				header = "ID\tSTATE\tAGE\tSESSION\tDIR\tTASK\tLATEST"
			}
			_, _ = fmt.Fprintln(w, header)
			for _, t := range tasks {
				state, since, latest := describe(t, sessions, sessErr == nil, now)
				row := []string{t.Label(), state, age(now.Sub(since)), or(t.Session, "-")}
				if all {
					row = append(row, shortDir(t.Cwd))
				}
				title := t.Name
				if title == "" {
					title = queue.Summary(t.Prompt, 40)
				}
				row = append(row, title, latest)
				_, _ = fmt.Fprintln(w, strings.Join(row, "\t"))
			}
			if err := w.Flush(); err != nil {
				return err
			}
			return footer(c.OutOrStdout(), store, dataDir, sessions, now)
		},
	}
	cmd.Flags().BoolVar(&all, "all", false, "Show tasks from every project")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Show at most N most recent tasks (0 for all)")
	return cmd
}

func within(tasks []queue.Task, dir string) []queue.Task {
	var out []queue.Task
	for _, t := range tasks {
		if t.Cwd == dir || strings.HasPrefix(t.Cwd, dir+string(filepath.Separator)) {
			out = append(out, t)
		}
	}
	return out
}

// describe returns the display state of a task, the time its age counts from,
// and the latest assistant line of its session.
func describe(t queue.Task, sessions map[string]claude.Session, known bool, now time.Time) (string, time.Time, string) {
	switch t.State {
	case queue.Queued:
		return "queued", t.CreatedAt, ""
	case queue.Cancelled:
		return "cancelled", t.CreatedAt, ""
	case queue.Failed:
		return "launch failed", t.CreatedAt, queue.Summary(t.Error, 60)
	}
	latest := ""
	if t.Session != "" {
		if entries, err := claude.ReadTranscript(t.Session); err == nil {
			latest = queue.Summary(claude.LastAssistant(entries), 60)
		}
	}
	if !known {
		return "?", t.LaunchedAt, latest
	}
	s, ok := sessions[t.Session]
	switch {
	case ok:
		return sessionState(s), t.LaunchedAt, latest
	case t.Session == "" || now.Sub(t.LaunchedAt) < time.Minute:
		return "starting", t.LaunchedAt, latest
	default:
		return "gone", t.LaunchedAt, latest
	}
}

func sessionState(s claude.Session) string {
	switch s.State {
	case claude.StateBlocked:
		return "needs input"
	case "":
		return s.Status
	default:
		return s.State
	}
}

func footer(w io.Writer, store *queue.Store, dataDir string, sessions map[string]claude.Session, now time.Time) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	queued, err := store.List(queue.Queued)
	if err != nil {
		return err
	}
	launched, err := store.List(queue.Launched)
	if err != nil {
		return err
	}
	state := "stopped"
	if dispatch.Running(dataDir) {
		state = "running"
	}
	busy := "?"
	if sessions != nil {
		busy = fmt.Sprint(dispatch.Occupied(launched, sessions, now))
	}
	_, err = fmt.Fprintf(w, "\n%d queued · %s/%d slots busy · dispatcher %s (all projects)\n", len(queued), busy, cfg.Slots, state)
	return err
}

func or(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func age(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

func shortDir(dir string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home+string(filepath.Separator)) {
		return "~" + dir[len(home):]
	}
	return dir
}

// peekLines caps how much of one message peek prints.
const peekLines = 40

func peekCommand() *cobra.Command {
	var n int
	var tools bool
	cmd := &cobra.Command{
		Use:   "peek TASK",
		Short: "Show the recent conversation of a task's session",
		Long: "Show the last messages of a task's session without attaching to it. Tool\n" +
			"calls are hidden; --tools folds each run of them into a one-line count.",
		Example: "  csquad peek T3\n  csquad peek T3 -n 10 --tools",
		Args:    cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := queue.ParseID(args[0])
			if err != nil {
				return err
			}
			markExecuted(c)
			store, _, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			t, err := store.Get(id)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if t.State != queue.Launched || t.Session == "" {
				_, _ = fmt.Fprintf(out, "%s is %s; it has no session yet\n", t.Label(), t.State)
				if t.Error != "" {
					_, _ = fmt.Fprintln(out, t.Error)
				}
				return nil
			}
			state := "?"
			if sessions, err := claude.Sessions(c.Context()); err == nil {
				if s, ok := sessions[t.Session]; ok {
					state = sessionState(s)
				} else {
					state = "gone"
				}
			}
			_, _ = fmt.Fprintf(out, "%s · %s · %s · session %s · %s\n", t.Label(), state, age(time.Since(t.LaunchedAt)), t.Session, shortDir(t.Cwd))
			entries, err := claude.ReadTranscript(t.Session)
			if err != nil {
				return err
			}
			if !tools {
				kept := entries[:0:0]
				for _, e := range entries {
					if e.Role != claude.RoleTools {
						kept = append(kept, e)
					}
				}
				entries = kept
			}
			for _, e := range claude.Last(entries, n) {
				printEntry(out, e)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&n, "number", "n", 5, "Number of messages to show")
	cmd.Flags().BoolVar(&tools, "tools", false, "Show folded tool-call counts between messages")
	return cmd
}

func printEntry(w io.Writer, e claude.Entry) {
	if e.Role == claude.RoleTools {
		parts := make([]string, 0, len(e.Tools))
		for _, t := range e.Tools {
			parts = append(parts, fmt.Sprintf("%s ×%d", t.Name, t.Count))
		}
		_, _ = fmt.Fprintf(w, "  · %s\n", strings.Join(parts, ", "))
		return
	}
	_, _ = fmt.Fprintf(w, "\n[%s]\n", e.Role)
	lines := strings.Split(e.Text, "\n")
	for i, line := range lines {
		if i == peekLines {
			_, _ = fmt.Fprintf(w, "  … %d more lines\n", len(lines)-peekLines)
			break
		}
		_, _ = fmt.Fprintln(w, "  "+line)
	}
}

func cancelCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cancel TASK",
		Short: "Remove a queued task that has not started",
		Args:  cobra.ExactArgs(1),
		RunE: func(c *cobra.Command, args []string) error {
			id, err := queue.ParseID(args[0])
			if err != nil {
				return err
			}
			markExecuted(c)
			store, _, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			if err := store.Cancel(id); err != nil {
				return err
			}
			_, _ = fmt.Fprintf(c.OutOrStdout(), "T%d cancelled\n", id)
			return nil
		},
	}
}

func dispatchCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "dispatch",
		Short: "Run the dispatcher in the foreground",
		Long: "Launch queued tasks while slots are free, then exit once the queue is empty.\n" +
			"'csquad add' starts it automatically; run it by hand to watch its log.",
		Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			store, dataDir, err := openStore()
			if err != nil {
				return err
			}
			defer func() { _ = store.Close() }()
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()
			d := &dispatch.Dispatcher{Store: store, Config: cfg, Log: c.OutOrStdout()}
			err = d.Run(ctx, dataDir, pollInterval)
			if errors.Is(err, dispatch.ErrRunning) {
				_, _ = fmt.Fprintln(c.OutOrStdout(), "dispatcher already running")
				return nil
			}
			if errors.Is(err, context.Canceled) {
				return nil
			}
			return err
		},
	}
}

func doctorCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check Claude Code, paths and the dispatcher",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			out := c.OutOrStdout()
			var problems []string
			bin, err := claude.Binary()
			if err != nil {
				problems = append(problems, err.Error())
				_, _ = fmt.Fprintln(out, "claude:     missing")
			} else {
				_, _ = fmt.Fprintln(out, "claude:    ", bin)
				if _, err := claude.Sessions(c.Context()); err != nil {
					problems = append(problems, "claude agents --json failed: "+err.Error())
				}
			}
			cfgPath, _ := config.Path()
			cfg, err := config.Load()
			if err != nil {
				problems = append(problems, err.Error())
			}
			_, _ = fmt.Fprintf(out, "config:     %s (slots %d)\n", cfgPath, cfg.Slots)
			store, dataDir, err := openStore()
			if err != nil {
				problems = append(problems, err.Error())
			} else {
				_ = store.Close()
				state := "stopped"
				if dispatch.Running(dataDir) {
					state = "running"
				}
				_, _ = fmt.Fprintf(out, "database:   %s\ndispatcher: %s (log %s)\n", filepath.Join(dataDir, "csquad.db"), state, dispatch.LogPath(dataDir))
			}
			if len(problems) > 0 {
				return errors.New(strings.Join(problems, "\n"))
			}
			_, _ = fmt.Fprintln(out, "ok")
			return nil
		},
	}
}

func configCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "config",
		Short: "Show the effective configuration",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			path, err := config.Path()
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(c.OutOrStdout(), "# %s\nslots = %d\nmodel = %q\npermission_mode = %q\n", path, cfg.Slots, cfg.Model, cfg.PermissionMode)
			return err
		},
	}
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Show the CLI version",
		Args:  cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			_, err := fmt.Fprintln(c.OutOrStdout(), buildinfo.String())
			return err
		},
	}
}

func updateCommand() *cobra.Command {
	var check, yes bool
	cmd := &cobra.Command{
		Use:     "update",
		Short:   "Update csquad through the package manager that installed it",
		Example: "  csquad update --check\n  csquad update",
		Args:    cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			return update.Run(update.Options{Check: check, Yes: yes, Out: c.OutOrStdout()})
		},
	}
	cmd.Flags().BoolVar(&check, "check", false, "Report the installed and latest versions without installing anything")
	cmd.Flags().BoolVar(&yes, "yes", false, "Proceed without asking for confirmation")
	return cmd
}
