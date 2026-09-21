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
	d.Role = "Reviewer {{.Team}}"
	d.Handoff = "/tmp/{{.Engine}}/handoff"
	d.Cwd = "/tmp/{{.Generation}}"
	for _, entry := range []string{"startup", "runtime"} {
		text, err := Render(entry, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, literal := range []string{d.Instructions, d.Role, d.Handoff} {
			if !strings.Contains(text, literal) {
				t.Fatalf("%s reinterpreted %q", entry, literal)
			}
		}
		if strings.Contains(text, "Focus on the human's intent") {
			t.Fatal("data invoked master template")
		}
	}
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
				for _, want := range []string{
					"ordinary progress in task progress", "avoid a mistaken wait",
					"Developers and reviewers", "not message quotas", "Surface real blockers",
					"Source edits stop after submit", "Do not work past approval gates",
					"Reopen invalidates prior submission evidence", "Only the task owner writes",
					"bound to this session", "Acknowledge each message_id through message ack",
					"without copying secrets", d.Handoff,
				} {
					if !strings.Contains(text, want) {
						t.Fatalf("%s/%s/%s missing %q", engine, member, entry, want)
					}
				}
				if strings.Contains(text, "For uncertainty or missing permission") {
					t.Fatal("blanket escalation retained")
				}
				if strings.Contains(text, "task clean-worktree TASK --dry-run") != (member == "master") {
					t.Fatal("cleanup authority leaked across roles")
				}
				master := strings.Contains(text, "Review the current candidate SHA")
				worker := strings.Contains(text, "Workers MUST NOT ask the human")
				if master != (member == "master") || worker != (member != "master") {
					t.Fatalf("mixed role permissions: %s/%s", member, entry)
				}
				if strings.Contains(text, "Codex receives") != (engine == "codex") ||
					strings.Contains(text, "Claude receives") != (engine == "claude") {
					t.Fatal("wrong engine fragment")
				}
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
