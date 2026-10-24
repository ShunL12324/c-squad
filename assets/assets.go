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

//go:embed worker.md worker-codex.md flow.md skills
var files embed.FS

// Placeholder is replaced by the absolute path of the installed binary.
const Placeholder = "{{CSQUAD}}"

// Hosts are the agents the skills are installed for.
var Hosts = []string{"claude", "codex"}

// Skills returns the skills as name → SKILL.md text (unrendered, with the
// blocks of every host).
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

// SkillsFor returns the skills for one host: only its own blocks, which are
// marked like `<!-- codex -->` ... `<!-- /codex -->`.
func SkillsFor(host string) map[string]string {
	drop := map[string]bool{}
	for _, h := range Hosts {
		drop[h] = h != host
	}
	out := Skills()
	for name, text := range out {
		out[name] = filter(text, drop)
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

var marker = regexp.MustCompile(`^<!-- (/?)([a-z-]+) -->$`)

// filter drops the blocks whose tag drop holds and every marker line.
// Blocks do not nest.
func filter(text string, drop map[string]bool) string {
	var out []string
	skip := false
	for _, line := range strings.Split(text, "\n") {
		if m := marker.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if drop[m[2]] {
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

// WorkerRules returns worker.md with only the worktree or only the in-place
// blocks, which are marked by lines like `<!-- worktree -->` ...
// `<!-- /worktree -->`.
func WorkerRules(inPlace bool) string { return rules("worker.md", inPlace) }

// WorkerRulesFor returns the worker rules for a task of engine: Codex
// workers make their worktree themselves and ask by ending with a question.
func WorkerRulesFor(engine string, inPlace bool) string {
	if engine == "codex" {
		return rules("worker-codex.md", inPlace)
	}
	return WorkerRules(inPlace)
}

func rules(file string, inPlace bool) string {
	b, _ := files.ReadFile(file)
	return filter(string(b), map[string]bool{"in-place": !inPlace, "worktree": inPlace})
}

// FlowRules is what a task with a check or a merge adds to the worker
// rules: the messages csquad sends are the user's instructions.
func FlowRules() string {
	b, _ := files.ReadFile("flow.md")
	return string(b)
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
