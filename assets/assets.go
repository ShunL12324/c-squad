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

// Shared is the pseudo-host of a skills directory both hosts read (one
// linked to the other): every host's blocks stay, each labelled with its
// host.
const Shared = "shared"

// labels name the hosts in the shared rendering.
var labels = map[string]string{"claude": "Claude Code", "codex": "Codex"}

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

// SkillFiles returns every file of every skill as "name/SKILL.md",
// "name/reference.md", ... → text (unrendered, with the blocks of every
// host). A skill is SKILL.md plus files it reads on demand.
func SkillFiles() map[string]string {
	out := map[string]string{}
	_ = fs.WalkDir(files, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if b, err := files.ReadFile(p); err == nil {
			out[strings.TrimPrefix(p, "skills/")] = string(b)
		}
		return nil
	})
	return out
}

// SkillFilesFor returns SkillFiles for one host: only its own blocks, which
// are marked like `<!-- codex -->` ... `<!-- /codex -->`. For Shared every
// block stays, labelled.
func SkillFilesFor(host string) map[string]string {
	out := SkillFiles()
	for name, text := range out {
		out[name] = forHost(text, host)
	}
	return out
}

// SkillsFor returns the skills' SKILL.md for one host (or Shared).
func SkillsFor(host string) map[string]string {
	out := Skills()
	for name, text := range out {
		out[name] = forHost(text, host)
	}
	return out
}

func forHost(text, host string) string {
	if host == Shared {
		return shared(text)
	}
	return filter(text, hostDrop(host))
}

func hostDrop(host string) map[string]bool {
	drop := map[string]bool{}
	for _, h := range Hosts {
		drop[h] = h != host
	}
	return drop
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

// nonParagraph matches a line that does not start a plain paragraph: a
// fence, list item, quote, table, heading, HTML or indented code.
var nonParagraph = regexp.MustCompile("^( {4}|\t| *([-*+>|#<]|\\d+[.)]|```|~~~))")

// shared keeps every host block as a paragraph of its own, its first line
// starting "**Codex:** " (a block that does not start with a paragraph gets
// the label on a line of its own), and drops the marker lines.
func shared(text string) string {
	var out []string
	sep := func() {
		if len(out) > 0 && out[len(out)-1] != "" {
			out = append(out, "")
		}
	}
	label, first, closed := "", false, false
	for _, line := range strings.Split(text, "\n") {
		if m := marker.FindStringSubmatch(strings.TrimSpace(line)); m != nil {
			if l, ok := labels[m[2]]; ok {
				if m[1] == "" {
					sep()
					label, first, closed = l, true, false
				} else {
					label, first, closed = "", false, true
				}
			}
			continue
		}
		blank := strings.TrimSpace(line) == ""
		switch {
		case closed:
			// Whatever follows a block starts a paragraph of its own.
			if !blank {
				sep()
			}
			closed = false
		case first && blank:
			continue // the separating blank line is already there
		case first && nonParagraph.MatchString(line):
			out = append(out, "**"+label+":**", "")
			first = false
		case first:
			line = "**" + label + ":** " + line
			first = false
		}
		out = append(out, line)
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
// quotes, which bash, PowerShell and cmd.exe all understand around an
// argument; for the program itself see ProgramPath.
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

// ProgramPath is ShellPath for the program a command starts. PowerShell
// (Claude Code's shell on Windows without Git Bash, and Codex's) starts a
// quoted program only after `&`, which bash rejects; both expand a leading
// ~. So on Windows a path that needs quotes is written ~/... when it lies
// below home (the directory ~ names in both; "" for none) and the rest
// needs none.
func ProgramPath(p string, win bool, home string) string {
	q := ShellPath(p, win)
	if !win || home == "" || !strings.HasPrefix(q, `"`) {
		return q
	}
	p, home = strings.ReplaceAll(p, `\`, "/"), strings.TrimRight(strings.ReplaceAll(home, `\`, "/"), "/")
	if len(p) > len(home)+1 && strings.EqualFold(p[:len(home)], home) && p[len(home)] == '/' {
		if t := "~" + p[len(home):]; ShellPath(t, win) == t {
			return t
		}
	}
	return q
}

// Render fills in the binary's path, quoted for the shell (see ProgramPath
// for home).
func Render(text, binary string, win bool, home string) string {
	return strings.ReplaceAll(text, Placeholder, ProgramPath(binary, win, home))
}
