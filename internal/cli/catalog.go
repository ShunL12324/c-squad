package cli

import (
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/ShunL12324/c-squad/internal/config"
	"github.com/ShunL12324/c-squad/internal/tmux"
)

type definition struct {
	path, args, summary, example, complete string
	min, max                               int
	flags, required                        []string
	hidden                                 bool
	// compat keeps a second spelling parseable for existing scripts while help
	// recommends only one: start for new, the help group for question, and
	// reply for message reply.
	compat bool
}

var startupFlags = append([]string{"name", "profile", "detach", "color"}, removedLaunchFlags...)

// removedLaunchFlags name settings a profile now owns. They stay registered so
// an existing script fails with the explanation the command prints rather than a
// generic unknown-flag error, and they are hidden from help everywhere except
// doctor, which still probes one engine at a time.
var removedLaunchFlags = []string{"engine", "model", "env", "role", "template"}
var flagChoices = map[string][]string{"output": {"json", "table"}, "view": {"members", "tasks", "both", "hide"}, "color": tmux.ColorNames(), "engine": {string(config.Claude), string(config.Codex)}, "dispatch": {"assigned", "open"}, "kind": {"review", "test"}, "passed": {"true", "false"}, "direction": {"previous", "next"}}
var flagDescriptions = map[string]string{
	"discard-ignored": "Allow removal of ignored files listed by saved-team dry-run",
	"submission":      "Immutable task submission ID for evidence", "dry-run": "Show saved-team removal inventory without deleting",
	"output": "Query format: json or table (omitted preserves the existing default)",
	"cwd":    "Member startup directory; relative paths resolve from the calling directory",
	"view":   "Visible panels: members, tasks, both or hide", "popup": "Render a temporary popup panel",
	"color": "Member label color (random when omitted): " + strings.Join(tmux.ColorNames(), ", "), "name": "Team name or milestone name for this command", "engine": "Engine to probe; members and master take their engine from a profile", "model": "Removed; set model in a [profiles.NAME] table and select it with --profile", "env": "Removed; set the variables in that profile's [profiles.NAME.env] table", "detach": "Start without attaching this terminal", "fresh": "Start new native conversations while retaining the team ledger", "full": "Include the full message and event history", "role": "Removed; the member name is the label and --instructions describes the work", "instructions": "Responsibilities injected into system/developer context", "task": "Task ID", "profile": "Launch profile name defined by a [profiles.NAME] config table", "template": "Removed; select launch settings with --profile and supply responsibilities with --instructions", "prompt": "Explicit opening user message (none by default)", "description": "Task description", "acceptance": "Verifiable task acceptance criteria", "code": "Create an isolated Git worktree for this task", "milestones": "Comma-separated reporting checkpoints", "gates": "Comma-separated checkpoints requiring master approval", "deps": "Comma-separated prerequisite task IDs", "dispatch": "Assign an owner or allow members to claim the task", "request-id": "Stable idempotency key for retries", "setup": "Workspace setup instructions", "owner": "Member responsible for writing task code", "to": "Comma-separated task participants", "text": "Message or progress text", "summary": "Result or evidence summary", "sha": "Commit SHA: the review candidate, or the external commit for close-external", "kind": "Evidence category", "passed": "Whether verification passed", "all": "Broadcast to every available team member", "client": "tmux client identifier", "direction": "Member navigation direction", "index": "Member navigation index", "epoch": "Team incarnation for shutdown fencing", "expected-generation": "Master generation for shutdown fencing", "reason": "Reason recorded in the ledger for this operation", "repo": "Repository holding the real commit, outside the team repository", "strict": "Fail when required runtime tools are unavailable", "reprofile": "Re-read engine, model and environment from the member's profile in the current configuration (with --profile, from that profile)",
}

func addFlags(cmd *cobra.Command, names []string) {
	for _, name := range names {
		if fileInput(name) {
			cmd.Flags().String(name+"-file", "", "Read "+name+" from FILE, or '-' for stdin; exclusive with --"+name)
		}
	}
	for _, name := range names {
		description, ok := flagDescriptions[name]
		if !ok {
			panic("undocumented flag: " + name)
		}
		switch name {
		case "detach", "fresh", "full", "code", "all", "strict", "popup", "dry-run", "discard-ignored", "reprofile":
			cmd.Flags().Bool(name, false, description)
		case "name":
			if cmd.Name() == "new" || cmd.Name() == "start" || cmd.Name() == "csquad" {
				cmd.Flags().StringP(name, "s", "", "Name for the new team")
			} else {
				cmd.Flags().String(name, "", description)
			}
		case "env":
			cmd.Flags().StringArray(name, nil, description)
		default:
			cmd.Flags().String(name, "", description)
		}
		if fileInput(name) {
			cmd.MarkFlagsMutuallyExclusive(name, name+"-file")
		}
		completion := cobra.NoFileCompletions
		if name == "cwd" || name == "repo" {
			completion = func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
				return nil, cobra.ShellCompDirectiveFilterDirs
			}
		}
		if name == "name" && (cmd.Name() == "attach" || cmd.Name() == "resume" || cmd.Name() == "board" || cmd.Name() == "stop" || cmd.Name() == "recover" || cmd.Name() == "ui") {
			completion = completeResource("team")
		}
		if choices, ok := flagChoices[name]; ok {
			completion = cobra.FixedCompletions(choices, cobra.ShellCompDirectiveNoFileComp)
		}
		if name == "owner" || name == "to" {
			completion = completeResource("member")
		}
		if name == "task" || name == "deps" {
			completion = completeResource("task")
		}
		if err := cmd.RegisterFlagCompletionFunc(name, completion); err != nil {
			panic(err)
		}
		if name == "prompt" || removed(name) && cmd.Name() != "doctor" {
			if err := cmd.Flags().MarkHidden(name); err != nil {
				panic(err)
			}
		}
	}
}
func removed(flag string) bool {
	for _, name := range removedLaunchFlags {
		if name == flag {
			return true
		}
	}
	return false
}

// compatGroups are command groups kept only as alternative spellings.
var compatGroups = []string{"help"}

func groupDescription(name string) string {
	return map[string]string{"team": "Manage saved teams", "member": "Recruit, inspect, and manage team members", "task": "Publish, assign, review, and merge tasks", "message": "Send, receive, and acknowledge peer messages", "question": "Escalate a question to master or answer an escalation", "_internal": "Internal runtime operations (compatibility aliases)", "help": "Escalate a question to master or answer an escalation"}[name]
}
func definitions() []definition {
	defs := []definition{
		{path: "member set-cwd", args: " NAME", min: 1, max: 1, complete: "member", summary: "Repair a stopped team member directory before resume", flags: []string{"cwd"}, required: []string{"cwd"}},
		{path: "team remove", args: " NAME", min: 1, max: 1, complete: "team", summary: "Remove a stopped saved team after safety checks", flags: []string{"dry-run", "discard-ignored"}},
		{path: "new", args: " [NAME]", max: 1, summary: "Create a new team (tmux-style new -s NAME)", flags: startupFlags, example: "  csquad new -s research --profile codex\n  csquad new --detach"},
		{path: "start", args: " [NAME]", max: 1, compat: true, summary: "Create a new team; existing names require attach or resume", flags: startupFlags, example: "  csquad start my-team --profile claude-opus\n  csquad start --detach"},
		{path: "resume", args: " [TEAM]", max: 1, complete: "team", summary: "Resume a stopped/interrupted team and its conversations from the ledger", flags: []string{"name", "fresh", "detach", "env", "engine", "model"}, example: "  csquad resume my-team\n  csquad resume --fresh --detach"},
		{path: "attach", args: " [TEAM_OR_MEMBER]", summary: "Enter a running team without restarting; select a member with --name TEAM", max: 1, complete: "team-or-member", flags: []string{"name"}, example: "  csquad attach research\n  csquad attach --name research reviewer"},
		{path: "ui", summary: "Show team members and task panels", flags: []string{"view", "client", "name"}, example: "  csquad ui\n  csquad ui --view tasks\n  csquad ui --view hide"},
		{path: "ui-layout", hidden: true, flags: []string{"owner"}},
		{path: "ui-remember-layout", hidden: true, flags: []string{"owner"}},
		{path: "ui-toggle", hidden: true, flags: []string{"view", "client"}},
		{path: "ui-panel", hidden: true, flags: []string{"view", "owner", "popup"}},
		{path: "list", summary: "List saved teams and whether they are running"},
		{path: "board", summary: "Inspect team progress, blockers, and recent activity", flags: []string{"full", "name"}},
		{path: "config", summary: "Show effective user configuration"},
		{path: "doctor", summary: "Check tmux, Git, and native engine availability", flags: []string{"strict", "engine"}},
		{path: "version", summary: "Show the CLI version"},
		{path: "stop", args: " [TEAM]", max: 1, complete: "team", summary: "Stop team processes and retain recoverable work", flags: []string{"name"}},
		{path: "sync", summary: "Start the active runtime if needed and retry pending delivery"},
		{path: "reconcile", summary: "Reconcile durable Git merge intents"},
		{path: "recover", args: " [TEAM]", max: 1, complete: "team", summary: "Restart only master in an active team, from an outside terminal", flags: []string{"name", "fresh", "prompt", "reprofile", "profile"}},
		{path: "member add", args: " NAME", summary: "Recruit a member with a task-specific identity", min: 1, max: 1, flags: append([]string{"instructions", "profile", "task", "prompt", "color", "cwd"}, removedLaunchFlags...), example: "  csquad member add reviewer --profile claude-opus --instructions 'Review the candidate commit'"},
		{path: "member list", summary: "List team members"},
		{path: "member profiles", summary: "List launch profiles member add can use, without environment values"},
		{path: "task create", args: " TITLE...", summary: "Publish a task with acceptance criteria", min: 1, max: -1, flags: []string{"acceptance", "description", "code", "milestones", "gates", "deps", "dispatch", "request-id", "setup"}, required: []string{"acceptance"}, example: "  csquad task create 'Fix login' --code --acceptance 'Regression test passes' --request-id fix-login"},
		{path: "task list", summary: "List tasks"},
		{path: "task assign", args: " TASK", summary: "Assign the task owner and collaborators", min: 1, max: 1, complete: "task", flags: []string{"owner", "to"}, required: []string{"owner"}},
		{path: "task progress", args: " TASK", summary: "Report meaningful task progress", min: 1, max: 1, complete: "task", flags: []string{"text"}, required: []string{"text"}},
		{path: "task milestone", args: " TASK", summary: "Report a checkpoint", min: 1, max: 1, complete: "task", flags: []string{"name"}, required: []string{"name"}},
		{path: "task gate", args: " TASK", summary: "Approve a blocked checkpoint", min: 1, max: 1, complete: "task", flags: []string{"name"}, required: []string{"name"}},
		{path: "task submit", args: " TASK", summary: "Submit a candidate for review", min: 1, max: 1, complete: "task", flags: []string{"summary", "sha"}, required: []string{"summary"}},
		{path: "task evidence", args: " TASK", summary: "Record review or test evidence", min: 1, max: 1, complete: "task", flags: []string{"kind", "sha", "submission", "passed", "summary"}, required: []string{"kind", "passed", "summary"}},
		{path: "task clean-worktree", args: " TASK", summary: "Remove a safely merged task checkout while retaining its history and branch", min: 1, max: 1, complete: "task", flags: []string{"dry-run"}},
		{path: "task close-external", args: " TASK", summary: "Close a code task on a commit in another repository", min: 1, max: 1, complete: "task", flags: []string{"repo", "sha", "reason", "summary"}, required: []string{"repo", "sha", "reason"}, example: "  csquad task close-external T7 --repo /path/to/other-repo --sha 9f3c1ab --reason 'Work was pushed from the member repository; the task worktree was never used'"},
		{path: "message send", args: " MEMBER", summary: "Send a message to one member", min: 1, max: 1, complete: "member", flags: []string{"text", "task", "request-id"}, required: []string{"text"}},
		{path: "message broadcast", summary: "Broadcast to task participants or the team", flags: []string{"text", "task", "all", "request-id"}, required: []string{"text"}, example: "  csquad message broadcast --task T1 --text 'Report blockers'\n  csquad message broadcast --all --text 'Review the updated plan'"},
		{path: "message inbox", summary: "List message records not manually acknowledged or superseded (not an unread queue)"},
		{path: "reply", args: " MESSAGE", summary: "Reply to a received message", min: 1, max: 1, complete: "message", flags: []string{"text"}, required: []string{"text"}},
		{path: "help request", summary: "Ask master for a decision", flags: []string{"text", "task"}, required: []string{"text"}},
		{path: "help list", summary: "List escalated questions"},
		{path: "help answer", args: " QUESTION", summary: "Answer an escalation as master", min: 1, max: 1, complete: "question", flags: []string{"text"}, required: []string{"text"}},
	}
	for _, op := range []string{"inspect", "interrupt", "restart", "replace", "remove"} {
		d := definition{path: "member " + op, args: " MEMBER", summary: map[string]string{"inspect": "Inspect member state and process identity", "interrupt": "Interrupt the current turn", "restart": "Restart a member and resume its conversation", "replace": "Replace a member with a fresh conversation", "remove": "Remove a member while preserving work"}[op], min: 1, max: 1, complete: "member"}
		if op == "restart" || op == "replace" {
			d.flags = []string{"prompt", "cwd", "reprofile", "profile"}
		}
		defs = append(defs, d)
	}
	for _, op := range []string{"inspect", "claim", "approve", "merge", "reopen", "abort-merge", "brief"} {
		defs = append(defs, definition{path: "task " + op, args: " TASK", summary: map[string]string{"inspect": "Inspect one task", "claim": "Claim an available open task", "approve": "Approve the submitted candidate", "merge": "Merge an approved candidate", "reopen": "Reopen a submitted task for changes", "abort-merge": "Clear a safe-to-abort merge intent", "brief": "Request a task summary without changing its lifecycle"}[op], min: 1, max: 1, complete: "task"})
	}
	for _, op := range []string{"ack", "retry"} {
		defs = append(defs, definition{path: "message " + op, args: " MESSAGE", summary: map[string]string{"ack": "Acknowledge a received message", "retry": "Retry a message as master"}[op], min: 1, max: 1, complete: "message"})
	}
	for _, op := range []string{"runtime", "hook", "navigation"} {
		defs = append(defs, definition{path: op, hidden: true})
	}
	defs = append(defs, definition{path: "navigate", hidden: true, flags: []string{"client", "direction", "index"}}, definition{path: "shutdown", hidden: true, flags: []string{"epoch", "expected-generation", "reason"}, required: []string{"epoch", "expected-generation"}}, definition{path: "run-engine", hidden: true, max: -1})
	defs = append(defs, definition{path: "member attach", args: " MEMBER", summary: "Attach to an existing member terminal", min: 1, max: 1, complete: "member"})
	for i := range defs {
		if slices.Contains([]string{"list", "board", "member list", "member profiles", "member inspect", "task list", "task inspect", "message inbox", "help list"}, defs[i].path) {
			defs[i].flags = append(append([]string(nil), defs[i].flags...), "output")
		}
	}
	for i := range defs {
		if strings.HasPrefix(defs[i].path, "help ") || defs[i].path == "reply" {
			defs[i].compat = true
		}
	}
	original := append([]definition(nil), defs...)
	for _, d := range original {
		// The recommended spelling copied below is not itself a compatibility alias.
		d.compat = false
		switch {
		case strings.HasPrefix(d.path, "help "):
			d.path = strings.Replace(d.path, "help ", "question ", 1)
		case d.path == "reply":
			d.path = "message reply"
		case d.hidden:
			d.path = "_internal " + d.path
		default:
			continue
		}
		defs = append(defs, d)
	}
	return defs
}

func fileInput(name string) bool {
	return slices.Contains([]string{"text", "summary", "instructions", "description"}, name)
}
