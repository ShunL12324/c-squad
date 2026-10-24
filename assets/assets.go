// Package assets carries the files csquad ships: the worker rules and the
// skills, embedded in the binary.
package assets

import (
	"embed"
	"io/fs"
	"path"
	"regexp"
	"sort"
	"strings"
)

//go:embed worker.md skills
var files embed.FS

// Placeholder is replaced by the absolute path of the installed binary.
const Placeholder = "{{CSQUAD}}"

// Skills returns the skills as name → SKILL.md text (unrendered).
func Skills() map[string]string {
	out := map[string]string{}
	entries, _ := fs.ReadDir(files, "skills")
	for _, e := range entries {
		if b, err := files.ReadFile(path.Join("skills", e.Name(), "SKILL.md")); err == nil {
			out[e.Name()] = string(b)
		}
	}
	return out
}

// SkillNames returns the skill names, sorted.
func SkillNames() []string {
	var names []string
	for n := range Skills() {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var marker = regexp.MustCompile(`^<!-- (/?)(worktree|in-place) -->$`)

// WorkerRules returns worker.md with only the worktree or only the in-place
// blocks, which are marked by lines like `<!-- worktree -->` ...
// `<!-- /worktree -->`.
func WorkerRules(inPlace bool) string {
	b, _ := files.ReadFile("worker.md")
	drop := "in-place"
	if inPlace {
		drop = "worktree"
	}
	var out []string
	skip := false
	for _, line := range strings.Split(string(b), "\n") {
		if m := marker.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if m[2] == drop {
				skip = m[1] == ""
			}
			continue
		}
		if !skip {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// ShellPath quotes a path for the shells a skill's command may run in. On
// Windows the path uses forward slashes (every shell accepts them) and double
// quotes, which bash, PowerShell and cmd.exe all understand.
func ShellPath(p string, win bool) string {
	if win {
		p = strings.ReplaceAll(p, `\`, "/")
		if regexp.MustCompile(`^[\w@+=:,./~-]+$`).MatchString(p) {
			return p
		}
		return `"` + p + `"`
	}
	if regexp.MustCompile(`^[\w@%+=:,./-]+$`).MatchString(p) {
		return p
	}
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// Render fills in the binary's path, quoted for the shell.
func Render(text, binary string, win bool) string {
	return strings.ReplaceAll(text, Placeholder, ShellPath(binary, win))
}
