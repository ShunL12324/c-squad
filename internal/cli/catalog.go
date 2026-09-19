package cli

import (
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
}

var startupFlags = []string{"name", "engine", "model", "env", "detach", "color"}
var flagChoices = map[string][]string{"view": {"members", "tasks", "both", "hide"}, "color": tmux.ColorNames(), "engine": {string(config.Claude), string(config.Codex)}, "dispatch": {"assigned", "open"}, "kind": {"review", "test"}, "passed": {"true", "false"}, "direction": {"previous", "next"}}
var flagDescriptions = map[string]string{
	"view": "Visible panels: members, tasks, both or hide", "popup": "Render a temporary popup panel",
	"color": "Member label color (random when omitted): " + strings.Join(tmux.ColorNames(), ", "), "name": "Team name or milestone name for this command", "engine": "Native agent engine", "model": "Native engine model name", "env": "Environment override KEY=VALUE; repeat for multiple values", "detach": "Start without attaching this terminal", "fresh": "Start new native conversations while retaining the team ledger", "full": "Include the full message and event history", "role": "Free-form member identity", "instructions": "Responsibilities injected into system/developer context", "task": "Task ID", "template": "Legacy role template name", "prompt": "Explicit opening user message (none by default)", "description": "Task description", "acceptance": "Verifiable task acceptance criteria", "code": "Create an isolated Git worktree for this task", "milestones": "Comma-separated reporting checkpoints", "gates": "Comma-separated checkpoints requiring master approval", "deps": "Comma-separated prerequisite task IDs", "dispatch": "Assign an owner or allow members to claim the task", "request-id": "Stable idempotency key for retries", "setup": "Workspace setup instructions", "owner": "Member responsible for writing task code", "to": "Comma-separated task participants", "text": "Message or progress text", "summary": "Result or evidence summary", "sha": "Candidate commit SHA", "kind": "Evidence category", "passed": "Whether verification passed", "all": "Broadcast to every available team member", "client": "tmux client identifier", "direction": "Member navigation direction", "index": "Member navigation index", "epoch": "Team incarnation for shutdown fencing", "expected-generation": "Master generation for shutdown fencing", "reason": "Shutdown reason", "strict": "Fail when required runtime tools are unavailable",
}

func addFlags(cmd *cobra.Command, names []string) {
	for _, name := range names {
		description, ok := flagDescriptions[name]
		if !ok {
			panic("undocumented flag: " + name)
		}
		switch name {
		case "detach", "fresh", "full", "code", "all", "strict", "popup":
			cmd.Flags().Bool(name, false, description)
		case "env":
			cmd.Flags().StringArray(name, nil, description)
		default:
			cmd.Flags().String(name, "", description)
		}
		completion := cobra.NoFileCompletions
		if name == "name" && (cmd.Name() == "attach" || cmd.Name() == "resume" || cmd.Name() == "board" || cmd.Name() == "stop" || cmd.Name() == "ui") {
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
		if name == "template" || name == "prompt" {
			if err := cmd.Flags().MarkHidden(name); err != nil {
				panic(err)
			}
		}
	}
}
func groupDescription(name string) string {
	return map[string]string{"member": "Recruit, inspect, and manage team members", "task": "Publish, assign, review, and merge tasks", "message": "Send, receive, and acknowledge peer messages", "help": "Escalate a question to master or answer an escalation"}[name]
}
func definitions() []definition {
	defs := []definition{
		{path: "start", summary: "Start a master session in tmux", flags: startupFlags, example: "  csquad start --name my-team --engine claude\n  csquad start --detach --env CODEX_HOME=/path/to/codex-home"},
		{path: "resume", summary: "Recover a stopped team from its ledger", flags: []string{"name", "env", "fresh", "detach"}, example: "  csquad resume --name my-team\n  csquad resume --fresh --detach"},
		{path: "attach", args: " [TEAM_OR_MEMBER]", summary: "Enter a team, or select a member with --name TEAM", max: 1, complete: "team-or-member", flags: []string{"name"}, example: "  csquad attach research\n  csquad attach --name research reviewer"},
		{path: "ui", summary: "Show team members and task panels", flags: []string{"view", "client", "name"}, example: "  csquad ui\n  csquad ui --view tasks\n  csquad ui --view hide"},
		{path: "ui-layout", hidden: true},
		{path: "ui-toggle", hidden: true, flags: []string{"view", "client"}},
		{path: "ui-panel", hidden: true, flags: []string{"view", "owner", "popup"}},
		{path: "list", summary: "List saved teams and whether they are running"},
		{path: "board", summary: "Inspect team progress, blockers, and recent activity", flags: []string{"full", "name"}},
		{path: "config", summary: "Show effective user configuration"},
		{path: "doctor", summary: "Check tmux, Git, and native engine availability", flags: []string{"strict", "engine"}},
		{path: "version", summary: "Show the CLI version"},
		{path: "stop", summary: "Stop team processes and retain recoverable work", flags: []string{"name"}},
		{path: "sync", summary: "Retry pending message delivery"},
		{path: "reconcile", summary: "Reconcile durable Git merge intents"},
		{path: "recover", summary: "Restart master from an outside terminal", flags: []string{"fresh", "prompt"}},
		{path: "member add", args: " NAME", summary: "Recruit a member with a task-specific identity", min: 1, max: 1, flags: []string{"role", "instructions", "engine", "model", "env", "task", "template", "prompt", "color"}, example: "  csquad member add reviewer --engine claude --role reviewer --instructions 'Review the candidate commit'"},
		{path: "member list", summary: "List team members"},
		{path: "task create", args: " TITLE...", summary: "Publish a task with acceptance criteria", min: 1, max: -1, flags: []string{"acceptance", "description", "code", "milestones", "gates", "deps", "dispatch", "request-id", "setup"}, required: []string{"acceptance"}, example: "  csquad task create 'Fix login' --code --acceptance 'Regression test passes' --request-id fix-login"},
		{path: "task list", summary: "List tasks"},
		{path: "task assign", args: " TASK", summary: "Assign the task owner and collaborators", min: 1, max: 1, complete: "task", flags: []string{"owner", "to"}, required: []string{"owner"}},
		{path: "task progress", args: " TASK", summary: "Report meaningful task progress", min: 1, max: 1, complete: "task", flags: []string{"text"}, required: []string{"text"}},
		{path: "task milestone", args: " TASK", summary: "Report a checkpoint", min: 1, max: 1, complete: "task", flags: []string{"name"}, required: []string{"name"}},
		{path: "task gate", args: " TASK", summary: "Approve a blocked checkpoint", min: 1, max: 1, complete: "task", flags: []string{"name"}, required: []string{"name"}},
		{path: "task submit", args: " TASK", summary: "Submit a candidate for review", min: 1, max: 1, complete: "task", flags: []string{"summary", "sha"}, required: []string{"summary"}},
		{path: "task evidence", args: " TASK", summary: "Record review or test evidence", min: 1, max: 1, complete: "task", flags: []string{"kind", "sha", "passed", "summary"}, required: []string{"kind", "sha", "passed", "summary"}},
		{path: "message send", args: " MEMBER", summary: "Send a message to one member", min: 1, max: 1, complete: "member", flags: []string{"text", "task", "request-id"}, required: []string{"text"}},
		{path: "message broadcast", summary: "Broadcast to task participants or the team", flags: []string{"text", "task", "all", "request-id"}, required: []string{"text"}, example: "  csquad message broadcast --task T1 --text 'Report blockers'\n  csquad message broadcast --all --text 'Review the updated plan'"},
		{path: "message inbox", summary: "List unacknowledged messages for the calling member"},
		{path: "reply", args: " MESSAGE", summary: "Reply to a received message", min: 1, max: 1, complete: "message", flags: []string{"text"}, required: []string{"text"}},
		{path: "help request", summary: "Ask master for a decision", flags: []string{"text", "task"}, required: []string{"text"}},
		{path: "help list", summary: "List escalated questions"},
		{path: "help answer", args: " QUESTION", summary: "Answer an escalation as master", min: 1, max: 1, complete: "question", flags: []string{"text"}, required: []string{"text"}},
	}
	for _, op := range []string{"inspect", "interrupt", "restart", "replace", "remove"} {
		d := definition{path: "member " + op, args: " MEMBER", summary: map[string]string{"inspect": "Inspect member state and process identity", "interrupt": "Interrupt the current turn", "restart": "Restart a member and resume its conversation", "replace": "Replace a member with a fresh conversation", "remove": "Remove a member while preserving work"}[op], min: 1, max: 1, complete: "member"}
		if op == "restart" || op == "replace" {
			d.flags = []string{"prompt"}
		}
		defs = append(defs, d)
	}
	for _, op := range []string{"inspect", "claim", "approve", "merge", "reopen", "abort-merge"} {
		defs = append(defs, definition{path: "task " + op, args: " TASK", summary: map[string]string{"inspect": "Inspect one task", "claim": "Claim an available open task", "approve": "Approve the submitted candidate", "merge": "Merge an approved candidate", "reopen": "Reopen a submitted task for changes", "abort-merge": "Clear a safe-to-abort merge intent"}[op], min: 1, max: 1, complete: "task"})
	}
	for _, op := range []string{"ack", "retry"} {
		defs = append(defs, definition{path: "message " + op, args: " MESSAGE", summary: map[string]string{"ack": "Acknowledge a received message", "retry": "Retry a message as master"}[op], min: 1, max: 1, complete: "message"})
	}
	for _, op := range []string{"runtime", "hook", "navigation"} {
		defs = append(defs, definition{path: op, hidden: true})
	}
	defs = append(defs, definition{path: "navigate", hidden: true, flags: []string{"client", "direction", "index"}}, definition{path: "shutdown", hidden: true, flags: []string{"epoch", "expected-generation", "reason"}, required: []string{"epoch", "expected-generation"}}, definition{path: "run-engine", hidden: true, min: 1, max: -1})
	return defs
}
