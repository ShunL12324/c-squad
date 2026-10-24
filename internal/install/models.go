package install

import (
	"os"
	"regexp"
	"strings"
)

// ~/.csquad/models.md is the user's own model routing, never written by
// csquad. The create-task skill reads it only once it is in use, so its
// "## When to use create-task" section (trigger phrases) is appended to the
// installed skill's description by install and update.

var triggersHeading = regexp.MustCompile(`(?im)^##[ \t]+when to use create-task[ \t]*$`)

// Triggers returns the "## When to use create-task" section of models.md as
// one line, or "".
func Triggers(models string) string {
	b, err := os.ReadFile(models)
	if err != nil {
		return ""
	}
	text := strings.ReplaceAll(string(b), "\r\n", "\n")
	loc := triggersHeading.FindStringIndex(text)
	if loc == nil {
		return ""
	}
	section := text[loc[1]:]
	if i := regexp.MustCompile(`(?m)^#`).FindStringIndex(section); i != nil {
		section = section[:i[0]]
	}
	return strings.Join(strings.Fields(section), " ")
}

// extendDescription appends triggers to a SKILL.md's description, written as
// a double-quoted YAML string.
func extendDescription(text, triggers string) string {
	lines := strings.Split(text, "\n")
	for i, l := range lines {
		if i > 0 && l == "---" {
			break
		}
		if v, ok := strings.CutPrefix(l, "description:"); ok {
			full := strings.TrimSpace(strings.TrimSpace(v) + " " + triggers)
			lines[i] = "description: " + yamlQuote(full)
			break
		}
	}
	return strings.Join(lines, "\n")
}

// yamlQuote writes s as a YAML double-quoted scalar.
func yamlQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
