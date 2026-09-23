package squad

import (
	"strings"
	"testing"
)

// A member told about a task already knows its ID, so the notice points at
// task inspect, which returns that task's workspace and acceptance, rather than
// at the whole-team board.
func TestTaskNoticesPointAtTaskInspect(t *testing.T) {
	st := testStore(t)
	must(t, taskCommand(st, "master", []string{"create", "assigned work"}, options{"acceptance": "done"}))
	must(t, taskCommand(st, "master", []string{"create", "open work"}, options{"acceptance": "done", "dispatch": "open"}))
	s, err := st.read()
	must(t, err)
	ids := map[string]string{}
	for id, task := range s.Tasks {
		ids[task.Title] = id
	}
	assigned := ids["assigned work"]
	must(t, taskCommand(st, "master", []string{"assign", assigned}, options{"owner": "a"}))
	s, err = st.read()
	must(t, err)
	want := map[string]string{
		"a": "Assigned to task " + assigned + ". Run task inspect " + assigned + " ",
		"b": "Task available: " + ids["open work"] + " open work. Run task inspect " + ids["open work"] + " and claim if suitable.",
	}
	for _, m := range s.Messages {
		if prefix, ok := want[m.To]; ok && strings.HasPrefix(m.Text, prefix) {
			if strings.Contains(m.Text, "Read board") {
				t.Fatalf("notice still sends %s to the board: %q", m.To, m.Text)
			}
			delete(want, m.To)
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing notices %v in %+v", want, s.Messages)
	}
}
