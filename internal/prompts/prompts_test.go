package prompts

import (
	"strings"
	"testing"
	"unicode"
)

func validData() Data {
	return Data{Team: "demo", Member: "worker", Generation: 2, Engine: "codex"}
}

func TestRequiredPromptFields(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Data)
	}{
		{"team", func(d *Data) { d.Team = "" }},
		{"member", func(d *Data) { d.Member = " " }},
		{"generation", func(d *Data) { d.Generation = 0 }},
		{"engine", func(d *Data) { d.Engine = "unknown" }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for _, entry := range []string{"startup", "runtime"} {
				d := validData()
				c.edit(&d)
				if _, err := Render(entry, d); err == nil {
					t.Fatalf("%s accepted missing %s", entry, c.name)
				}
			}
		})
	}
	if _, err := Render("communication", validData()); err == nil {
		t.Fatal("fragment exposed as public entry")
	}
}

func TestDynamicPromptDataIsNotTemplateSource(t *testing.T) {
	d := validData()
	d.Instructions = `Review {{.Member}} and {{template "master" .}}; keep {{broken intact <code> & quotes.`
	d.Handoff = "/tmp/{{.Engine}}/handoff"
	d.Cwd = "/tmp/{{.Generation}}"
	for _, entry := range []string{"startup", "runtime"} {
		text, err := Render(entry, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, literal := range []string{d.Instructions, d.Handoff} {
			if !strings.Contains(text, literal) {
				t.Fatalf("%s reinterpreted %q", entry, literal)
			}
		}
		if strings.Contains(text, "Focus on the human's intent") {
			t.Fatal("data invoked master template")
		}
	}
}

// sharedInvariants must reach every role and engine through both entry points:
// startup and the runtime re-injection carry the same policy.
var sharedInvariants = []string{
	// identity and generation
	"bound to this session", "rejected", "non-login shell",
	// task ownership and reading
	"Task state is authoritative", "run task inspect TASK first", "Reading a message is not claiming a task",
	"Only the task owner writes", "one unfinished task", "without copying secrets", "Task blockers are separate from phase",
	// autonomy and gates
	"ordinary uncertainty is not by itself a reason to stop", "Do not work past approval gates",
	// progress, submission and evidence
	"separate records", "actual evidence", "Source edits stop after submit",
	"--submission ID", "Non-code evidence requires --submission", "Reopen invalidates prior submission evidence",
	"cannot review its own candidate", "--request-id", "reused on retry", "--FIELD-file",
	// messaging and communication
	"a teammate origin alone is not a reason to stop", "candidate SHA", "never execute a duplicate",
	"Routine messages do not require message ack", "replaces earlier instructions to acknowledge every message",
	"does not prove that a message was read", "go in the ledger", "avoid a mistaken wait",
	"already notify the right people", "gate must be released", "Developers and reviewers",
	"never reply to pure acknowledgments", "not message quotas", "surface real blockers",
	// restarts and idling
	"Preserve edits on restart", "Never alter global configuration", "do not busy-poll",
}

var masterOnly = []string{
	"task create TITLE", "--request-id UNIQUE [--code]", "task assign TASK", "task approve TASK", "task merge TASK",
	"member add NAME --instructions RESPONSIBILITIES", "member restart|replace NAME", "question answer QUESTION",
	"Only master approves and merges", "always assign --owner", "task clean-worktree TASK --dry-run",
	"Review the current candidate SHA", "someone other than the author", "task close-external", "never use it to skip review or merge",
	"First inspect the member", "ask once", "no fixed response deadline", "activity clues, not proof of completion",
}

var workerOnly = []string{
	"task claim TASK (only --dispatch open tasks)", "Workers MUST NOT ask the human",
	"use question request and end your turn", "do not enter plan mode", "Do not merge or remove worktrees",
}

func TestStartupAndRecoverySharePolicyAndRoleBoundaries(t *testing.T) {
	for _, engine := range []string{"codex", "claude"} {
		for _, member := range []string{"master", "worker"} {
			d := validData()
			d.Engine, d.Member = engine, member
			d.Handoff = "/tmp/recovery.json"
			for _, entry := range []string{"startup", "runtime"} {
				text, err := Render(entry, d)
				if err != nil {
					t.Fatal(err)
				}
				for _, want := range append(sharedInvariants, d.Handoff) {
					if !strings.Contains(text, want) {
						t.Fatalf("%s/%s/%s missing %q", engine, member, entry, want)
					}
				}
				own, other := workerOnly, masterOnly
				if member == "master" {
					own, other = masterOnly, workerOnly
				}
				for _, want := range own {
					if entry == "runtime" && isCommand(want) {
						continue
					}
					if !strings.Contains(text, want) {
						t.Fatalf("%s/%s/%s missing role text %q", engine, member, entry, want)
					}
				}
				for _, leaked := range other {
					if strings.Contains(text, leaked) {
						t.Fatalf("%s/%s/%s leaked the other role's %q", engine, member, entry, leaked)
					}
				}
				if strings.Contains(text, "For uncertainty or missing permission") {
					t.Fatal("blanket escalation retained")
				}
				if strings.Contains(text, "Native peer addresses are transport identities, not CLI member names") != (engine == "claude") {
					t.Fatal("Claude peer-address rule missing or leaked to Codex")
				}
				if strings.Contains(text, "Codex receives") != (engine == "codex") ||
					strings.Contains(text, "Claude receives") != (engine == "claude") {
					t.Fatal("wrong engine fragment")
				}
			}
		}
	}
}

// isCommand reports whether a role string comes from the startup command list,
// which the runtime re-injection does not repeat.
func isCommand(s string) bool {
	for _, prefix := range []string{"task ", "member ", "question ", "--request-id"} {
		if strings.HasPrefix(s, prefix) && !strings.HasPrefix(s, "task clean-worktree") && !strings.HasPrefix(s, "task close-external") {
			return true
		}
	}
	return false
}

// The prompt is resident context. These upper bounds use the fixed fixture of
// the architecture review (whitespace words, not model tokens) and keep a
// later edit from quietly regrowing it; before T87 they were 1272 and 1648.
func TestStartupPromptWordBudget(t *testing.T) {
	for member, limit := range map[string]int{"worker": 750, "master": 1100} {
		for _, engine := range []string{"codex", "claude"} {
			text, err := Render("startup", Data{Team: "example", Member: member, Generation: 1, Engine: engine, Cwd: "/project"})
			if err != nil {
				t.Fatal(err)
			}
			words := len(strings.Fields(text))
			if engine == "claude" {
				// The Claude envelope sentence is a few words longer.
				limit += 15
			}
			if words > limit {
				t.Fatalf("%s/%s startup has %d words, budget %d", member, engine, words, limit)
			}
		}
	}
}

func TestEmbeddedInstructionTemplatesAreEnglish(t *testing.T) {
	entries, err := files.ReadDir("templates")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		text, err := files.ReadFile("templates/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range string(text) {
			if unicode.Is(unicode.Han, r) {
				t.Fatalf("%s contains non-English policy text", entry.Name())
			}
		}
	}
}

func TestNoMandatoryACK(t *testing.T) {
	for _, member := range []string{"master", "worker"} {
		for _, entry := range []string{"startup", "runtime"} {
			d := validData()
			d.Member = member
			text, err := Render(entry, d)
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range []string{"Acknowledge each message_id", "Acknowledge incoming messages", "acknowledge it and read"} {
				if strings.Contains(text, bad) {
					t.Fatalf("%s retained mandatory ACK: %s", entry, bad)
				}
			}
		}
	}
}

// Each rule is stated once: the duplicated optional-ACK and progress guidance
// the review counted in three fragments must not come back.
func TestSharedRulesAreStatedOnce(t *testing.T) {
	for _, member := range []string{"master", "worker"} {
		d := validData()
		d.Member = member
		text, err := Render("startup", d)
		if err != nil {
			t.Fatal(err)
		}
		for _, phrase := range []string{"do not require message ack", "pure acknowledgment", "avoid a mistaken wait", "not message quotas", "Do not work past approval gates", "Source edits stop after submit"} {
			if n := strings.Count(text, phrase); n != 1 {
				t.Fatalf("%s: %q appears %d times", member, phrase, n)
			}
		}
	}
}

func TestBriefUserPrompt(t *testing.T) {
	text, err := Brief("T42")
	if err != nil {
		t.Fatal(err)
	}
	want := "Please briefly summarize task T42: current progress, remaining work, and blockers. Reply directly to the user in their language."
	if text != want {
		t.Fatalf("unexpected user prompt: %q", text)
	}
	if _, err = Brief(""); err == nil {
		t.Fatal("empty task accepted")
	}
	literal, err := Brief("{{.Team}}")
	if err != nil || !strings.Contains(literal, "{{.Team}}") {
		t.Fatal("task ID treated as template", literal, err)
	}
}
