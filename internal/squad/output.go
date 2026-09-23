package squad

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
)

// instructionsSummaryWidth keeps the member table readable on a narrow terminal.
const instructionsSummaryWidth = 48

// queryOut keeps JSON as the established query default. Tables are concise
// human-readable projections; JSON remains the complete machine representation.
func queryOut(o options, value any) error {
	if o["output"] != "table" {
		return jsonOut(value)
	}
	return writeTable(os.Stdout, value)
}

func writeTable(out io.Writer, value any) error {
	switch data := value.(type) {
	case map[string]*Member:
		rows := [][]string{}
		for _, id := range sortedKeys(data) {
			m := data[id]
			rows = append(rows, []string{id, string(m.State), string(m.Engine), instructionsSummary(m.Instructions), m.Cwd})
		}
		return writeRows(out, []string{"MEMBER", "STATE", "ENGINE", "INSTRUCTIONS", "DIRECTORY"}, rows)
	case map[string]*Task:
		rows := [][]string{}
		for _, id := range sortedKeys(data) {
			task := data[id]
			rows = append(rows, []string{id, string(task.State), task.Owner, task.Title, strings.Join(task.Blockers, ", ")})
		}
		return writeRows(out, []string{"TASK", "STATE", "OWNER", "TITLE", "BLOCKERS"}, rows)
	case map[string]*Question:
		rows := [][]string{}
		for _, id := range sortedKeys(data) {
			q := data[id]
			rows = append(rows, []string{id, string(q.State), q.Member, q.Task, q.Text})
		}
		return writeRows(out, []string{"QUESTION", "STATE", "MEMBER", "TASK", "TEXT"}, rows)
	case []*Message:
		rows := [][]string{}
		for _, m := range data {
			rows = append(rows, []string{m.ID, string(m.State), m.From, m.To, m.Task, m.Text})
		}
		return writeRows(out, []string{"MESSAGE", "STATE", "FROM", "TO", "TASK", "TEXT"}, rows)
	case profileListing:
		rows := [][]string{}
		for _, p := range data.Profiles {
			selected := []string{}
			if p.Name == data.DefaultProfile {
				selected = append(selected, "member default")
			}
			if p.Name == data.MasterProfile {
				selected = append(selected, "master")
			}
			command := "engine name"
			if p.CustomCommand {
				command = "custom"
			}
			rows = append(rows, []string{p.Name, string(p.Engine), p.Model, command, strings.Join(p.EnvKeys, ", "), strings.Join(selected, ", ")})
		}
		return writeRows(out, []string{"PROFILE", "ENGINE", "MODEL", "COMMAND", "ENV", "SELECTED"}, rows)
	case *State:
		return writeBoardTable(out, map[string]any{"team": data.ID, "phase": data.Phase, "active": data.Active, "root": data.Root, "members": data.Members, "tasks": data.Tasks, "questions": data.Questions})
	case map[string]any:
		if _, ok := data["members"].(map[string]*Member); ok {
			return writeBoardTable(out, data)
		}
	}

	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	var v any
	if err = json.Unmarshal(raw, &v); err != nil {
		return err
	}
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err = fmt.Fprintln(w, "FIELD\tVALUE"); err != nil {
		return err
	}
	emit := func(key string, value any) error {
		// JSON quoting prevents embedded tabs/newlines from corrupting table rows.
		b, err := json.Marshal(value)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "%s\t%s\n", strconv.Quote(key), b)
		return err
	}
	switch data := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(data))
		for key := range data {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err = emit(key, data[key]); err != nil {
				return err
			}
		}
	case []any:
		for i, row := range data {
			if err = emit(strconv.Itoa(i), row); err != nil {
				return err
			}
		}
	default:
		if err = emit("value", data); err != nil {
			return err
		}
	}
	return w.Flush()
}

// instructionsSummary reduces responsibilities to one table cell. Instructions
// are written as prose and often span paragraphs, so only the first line is
// kept, its internal whitespace collapsed, and the result cut to a width that
// still leaves room for the directory column. member inspect shows the rest.
func instructionsSummary(text string) string {
	line, _, _ := strings.Cut(text, "\n")
	summary := strings.Join(strings.Fields(line), " ")
	runes := []rune(summary)
	if len(runes) <= instructionsSummaryWidth {
		return summary
	}
	return string(runes[:instructionsSummaryWidth-1]) + "\u2026"
}

func sortedKeys[V any](items map[string]V) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func writeRows(out io.Writer, header []string, rows [][]string) error {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(w, strings.Join(header, "\t")); err != nil {
		return err
	}
	for _, row := range rows {
		cells := make([]string, len(row))
		for i, value := range row {
			// Quote control characters without adding quotes around ordinary text.
			escaped := strconv.Quote(value)
			cells[i] = escaped[1 : len(escaped)-1]
		}
		if _, err := fmt.Fprintln(w, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return w.Flush()
}

func writeBoardTable(out io.Writer, board map[string]any) error {
	if err := writeRows(out, []string{"TEAM", "PHASE", "ACTIVE", "ROOT"}, [][]string{{fmt.Sprint(board["team"]), fmt.Sprint(board["phase"]), fmt.Sprint(board["active"]), fmt.Sprint(board["root"])}}); err != nil {
		return err
	}
	for _, key := range []string{"members", "tasks", "questions"} {
		if _, err := fmt.Fprintln(out); err != nil {
			return err
		}
		if err := writeTable(out, board[key]); err != nil {
			return err
		}
	}
	return nil
}
