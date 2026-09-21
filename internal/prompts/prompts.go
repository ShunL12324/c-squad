// Package prompts renders the embedded English instructions used by C-Squad.
// Dynamic responsibilities and paths are data, never template source.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
)

// Revision invalidates the hook's once-per-session context marker when the
// embedded policy changes. It does not change member identity or permissions.
const Revision = "4"

//go:embed templates/*.tmpl
var files embed.FS

var templates, parseErr = template.New("prompts").Option("missingkey=error").ParseFS(files, "templates/*.tmpl")

// Data is the launch or recovery context. Role, instructions, cwd, colors and
// handoff may be empty; identity, generation and engine are required.
type Data struct {
	Team, Member, Role, Instructions, Engine, Cwd, Handoff, Colors string
	Generation                                                     int
}

// Render accepts only entry templates; fragments are implementation details.
func Render(entry string, data Data) (string, error) {
	if entry != "startup" && entry != "runtime" {
		return "", fmt.Errorf("unknown prompt entry %q", entry)
	}
	if strings.TrimSpace(data.Team) == "" || strings.TrimSpace(data.Member) == "" || data.Generation < 1 {
		return "", fmt.Errorf("prompt requires team, member and positive generation")
	}
	if data.Engine != "claude" && data.Engine != "codex" {
		return "", fmt.Errorf("prompt requires engine claude or codex")
	}
	if parseErr != nil {
		return "", fmt.Errorf("parse embedded prompts: %w", parseErr)
	}
	var out bytes.Buffer
	if err := templates.ExecuteTemplate(&out, entry, data); err != nil {
		return "", fmt.Errorf("render %s prompt: %w", entry, err)
	}
	return strings.TrimSpace(out.String()) + "\n", nil
}
