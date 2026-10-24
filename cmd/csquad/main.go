// Command csquad dispatches coding tasks (T1, T2, ...) to background Claude
// Code sessions and shows them in the status line.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/ShunL12324/c-squad/internal/app"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// A flag is "--name" (bool), "--name=" (takes a value) or "--name?" (takes
// a value when one follows); "-n|--n=" gives an alias.
type command struct {
	name  string
	usage string
	flags []string
	run   func(ctx context.Context, a *app.App, p *parsed) error
}

type parsed struct {
	values map[string]string
	bools  map[string]bool
	args   []string // positional
	rest   []string // after `--`
}

func (p *parsed) has(name string) bool { _, ok := p.values[name]; return ok || p.bools[name] }

var errUsage = errors.New("usage")

// orchestration is M3: the store and the table already hold its place.
var errLater = errors.New("comes with csquad's orchestration (dependencies, checks, merge) and is not available in this build")

func later(name string) func(context.Context, *app.App, *parsed) error {
	return func(context.Context, *app.App, *parsed) error {
		return fmt.Errorf("%s %w", name, errLater)
	}
}

var commands = []command{
	{
		name:  "launch",
		usage: "launch --title T [--cwd DIR] [--engine claude] [--model M] [--isolation none] [--prompt-file F [--rm-prompt]] [--no-worker-rules] [-- ENGINE_ARGS...]  (prompt on stdin)",
		flags: []string{"--title=", "--cwd=", "--engine=", "--model=", "--isolation=", "--prompt-file=", "--rm-prompt", "--no-worker-rules", "--after=", "--check=", "--max=", "--merge?"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			for _, f := range []string{"after", "check", "max", "merge"} {
				if p.has(f) {
					return fmt.Errorf("--%s %w", f, errLater)
				}
			}
			if len(p.args) > 0 {
				return fmt.Errorf("unexpected argument %q (the prompt comes on stdin or with --prompt-file)", p.args[0])
			}
			return a.Launch(ctx, app.LaunchArgs{
				Title: p.values["title"], Cwd: p.values["cwd"], Engine: p.values["engine"], Model: p.values["model"],
				Isolation: p.values["isolation"], PromptFile: p.values["prompt-file"], RmPrompt: p.bools["rm-prompt"],
				NoWorkerRules: p.bools["no-worker-rules"], EngineArgs: p.rest,
			})
		},
	},
	{
		name:  "status",
		usage: "status [--all] [--json]   this session's tasks: state, age, session, latest message",
		flags: []string{"--all", "--json"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			return a.Status(ctx, p.bools["all"], p.bools["json"])
		},
	},
	{
		name:  "peek",
		usage: "peek T12 [-n 6] [--all]   the task's recent conversation, tools folded",
		flags: []string{"-n|--n=", "--all"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			if len(p.args) != 1 {
				return errUsage
			}
			n := 6
			if v, ok := p.values["n"]; ok {
				var err error
				if n, err = strconv.Atoi(v); err != nil || n <= 0 {
					return fmt.Errorf("-n expects a positive number")
				}
			}
			return a.Peek(ctx, p.args[0], n, p.bools["all"])
		},
	},
	{
		name:  "wait",
		usage: "wait T12 [T13...] [--timeout MIN] [--all]   exits when one needs input, finishes, fails or is removed",
		flags: []string{"--timeout=", "--all"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			minutes := 240.0
			if v, ok := p.values["timeout"]; ok {
				var err error
				if minutes, err = strconv.ParseFloat(v, 64); err != nil {
					return fmt.Errorf("--timeout expects a number of minutes")
				}
			}
			return a.Wait(ctx, p.args, minutes, p.bools["all"])
		},
	},
	{
		name:  "send",
		usage: `send T12 "text" [--all]   a follow-up instruction (text from stdin when omitted)`,
		flags: []string{"--all"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			if len(p.args) < 1 {
				return errUsage
			}
			text := strings.Join(p.args[1:], " ")
			if len(p.args) == 1 {
				b, _ := io.ReadAll(a.Stdin)
				text = string(b)
			}
			return a.Send(ctx, p.args[0], strings.TrimSpace(text), p.bools["all"])
		},
	},
	{
		name:  "finish",
		usage: "finish T12 [T13...] [--into BRANCH] [--all]   verify merged + clean, then remove worktrees, branches and the session",
		flags: []string{"--into=", "--all"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			if len(p.args) == 0 {
				return errUsage
			}
			return a.Finish(ctx, p.args, p.values["into"], p.bools["all"])
		},
	},
	{
		name:  "cancel",
		usage: "cancel T12 [--discard] [--all]   stop a task and take it out of the list (keeps unmerged work unless --discard)",
		flags: []string{"--discard", "--all"},
		run: func(ctx context.Context, a *app.App, p *parsed) error {
			if len(p.args) != 1 {
				return errUsage
			}
			return a.Cancel(ctx, p.args[0], p.bools["discard"], p.bools["all"])
		},
	},
	{name: "retry", usage: "retry T12   (orchestration; not in this build)", run: later("retry")},
	{name: "hook", usage: "hook claude-stop   (internal; orchestration)", flags: []string{"--home="}, run: later("hook")},
	{name: "check", usage: "check T12   (internal; orchestration)", run: later("check")},
	{name: "advance", usage: "advance [--stopped T12]   (internal; orchestration)", flags: []string{"--stopped="}, run: later("advance")},
	{
		name:  "statusline",
		usage: "statusline   the status line command (reads Claude Code's JSON on stdin)",
		run: func(_ context.Context, a *app.App, _ *parsed) error {
			a.Statusline()
			return nil
		},
	},
	{
		name:  "install",
		usage: "install   the binary to $CSQUAD_HOME/bin, the skills, the status line wrapper",
		run:   func(_ context.Context, a *app.App, _ *parsed) error { return a.Install(false) },
	},
	{
		name:  "update",
		usage: "update   the same, for an existing installation",
		run:   func(_ context.Context, a *app.App, _ *parsed) error { return a.Install(true) },
	},
	{
		name:  "uninstall",
		usage: "uninstall   restore the status line, remove what install wrote (keeps csquad.db and models.md)",
		run:   func(_ context.Context, a *app.App, _ *parsed) error { return a.Uninstall() },
	},
	{
		name:  "status-install",
		usage: "status-install   what is installed",
		run: func(_ context.Context, a *app.App, _ *parsed) error {
			a.InstallStatus()
			return nil
		},
	},
}

func find(name string) *command {
	for i := range commands {
		if commands[i].name == name {
			return &commands[i]
		}
	}
	return nil
}

func parse(c *command, argv []string) (*parsed, error) {
	p := &parsed{values: map[string]string{}, bools: map[string]bool{}}
	type spec struct {
		name string
		kind byte // 0 bool, '=' value, '?' optional value
	}
	specs := map[string]spec{}
	for _, f := range c.flags {
		names := strings.Split(f, "|")
		last := names[len(names)-1]
		var kind byte
		if strings.HasSuffix(last, "=") || strings.HasSuffix(last, "?") {
			kind = last[len(last)-1]
		}
		canonical := strings.TrimLeft(strings.TrimRight(last, "=?"), "-")
		for _, n := range names {
			specs[strings.TrimRight(n, "=?")] = spec{canonical, kind}
		}
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		if a == "--" {
			p.rest = argv[i+1:]
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			p.args = append(p.args, a)
			continue
		}
		name, value, inline := strings.Cut(a, "=")
		s, ok := specs[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag %s for %s", name, c.name)
		}
		switch s.kind {
		case 0:
			if inline {
				return nil, fmt.Errorf("%s takes no value", name)
			}
			p.bools[s.name] = true
		case '=':
			if !inline {
				if i+1 >= len(argv) {
					return nil, fmt.Errorf("%s needs a value", name)
				}
				i++
				value = argv[i]
			}
			p.values[s.name] = value
		case '?':
			if !inline && i+1 < len(argv) && !strings.HasPrefix(argv[i+1], "-") {
				i++
				value = argv[i]
			}
			p.values[s.name] = value
		}
	}
	return p, nil
}

func help(w io.Writer) {
	fmt.Fprintf(w, "csquad %s - background coding tasks (T1, T2, ...) for Claude Code\n\nUsage: csquad <command>\n\n", app.Version)
	names := make([]string, 0, len(commands))
	for _, c := range commands {
		names = append(names, c.name)
	}
	sort.Strings(names)
	for _, c := range commands {
		fmt.Fprintf(w, "  %s\n", c.usage)
	}
	fmt.Fprintf(w, "\n  version, help\n\n$CSQUAD_HOME (default ~/.csquad) holds csquad.db and the binary; $CLAUDE_CONFIG_DIR (default ~/.claude) gets the skills.\n")
}

func run(argv []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		help(stderr)
		return 2
	}
	name := argv[0]
	switch strings.TrimLeft(name, "-") {
	case "version", "v":
		fmt.Fprintln(stdout, app.Version)
		return 0
	case "help", "h":
		help(stdout)
		return 0
	}
	c := find(name)
	if c == nil {
		fmt.Fprintf(stderr, "csquad: unknown command %q\n\n", name)
		help(stderr)
		return 2
	}
	a := app.New(proc.Env(os.Environ()), stdin, stdout, stderr)
	defer a.Close()
	p, err := parse(c, argv[1:])
	if err == nil {
		err = c.run(context.Background(), a, p)
	}
	switch {
	case err == nil:
		return 0
	case c.name == "statusline":
		return 0
	case errors.Is(err, errUsage):
		fmt.Fprintf(stderr, "usage: csquad %s\n", c.usage)
		return 2
	case app.ErrSilent(err):
		return 1
	}
	fmt.Fprintf(stderr, "csquad: %v\n", err)
	return 1
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
