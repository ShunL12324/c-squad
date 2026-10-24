package assets

import (
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/shared from the skills")

// The shared rendering of every skill file is kept in testdata/shared, so a
// change to it is read in review: go test ./assets -run Golden -update.
func TestSharedRenderingGolden(t *testing.T) {
	got := SkillFilesFor(Shared)
	if *update {
		os.RemoveAll(filepath.Join("testdata", "shared"))
	}
	for name, text := range got {
		f := filepath.Join("testdata", "shared", filepath.FromSlash(name))
		if *update {
			os.MkdirAll(filepath.Dir(f), 0o755)
			if err := os.WriteFile(f, []byte(text), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(f)
		if err != nil {
			t.Errorf("%s: %v (run with -update)", name, err)
		} else if strings.ReplaceAll(string(want), "\r\n", "\n") != text {
			t.Errorf("%s differs from %s (run with -update and review the diff)", name, f)
		}
	}
	filepath.Walk(filepath.Join("testdata", "shared"), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			rel, _ := filepath.Rel(filepath.Join("testdata", "shared"), p)
			if _, ok := got[filepath.ToSlash(rel)]; !ok {
				t.Errorf("%s is not a skill file any more", p)
			}
		}
		return nil
	})
}

// Each host block of the source is one labelled paragraph in the shared
// rendering, outside any code fence; the rest reads as for either host.
func TestSharedRenderingLabelsEveryBlock(t *testing.T) {
	src, shared, claude := SkillFiles(), SkillFilesFor(Shared), SkillFilesFor("claude")
	front := regexp.MustCompile(`(?s)^---\n.*?\n---\n`)
	for name, text := range shared {
		if strings.Contains(text, "<!--") {
			t.Errorf("%s keeps a block marker", name)
		}
		if front.FindString(text) != front.FindString(claude[name]) {
			t.Errorf("%s: the frontmatter differs from Claude's", name)
		}
		lines := strings.Split(text, "\n")
		fence := false
		for host, label := range labels {
			want := strings.Count(src[name], "<!-- "+host+" -->")
			if got := strings.Count(text, "**"+label+":**"); got != want {
				t.Errorf("%s: %d %s labels for %d blocks", name, got, label, want)
			}
		}
		for i, line := range lines {
			if strings.HasPrefix(line, "```") {
				fence = !fence
			}
			if strings.Contains(line, ":**") && regexp.MustCompile(`\*\*(Claude Code|Codex):\*\*`).MatchString(line) {
				if fence || !regexp.MustCompile(`^\*\*(Claude Code|Codex):\*\*( \S|$)`).MatchString(line) || i == 0 || lines[i-1] != "" {
					t.Errorf("%s:%d: a label that does not start a paragraph: %q", name, i+1, line)
				}
			}
			if i > 0 && line == "" && lines[i-1] == "" {
				t.Errorf("%s:%d: two blank lines", name, i+1)
			}
		}
		if fence {
			t.Errorf("%s: unbalanced code fence", name)
		}
		// Without the labelled paragraphs, it is any host's rendering.
		unlabelled := regexp.MustCompile(`(?m)^\*\*(Claude Code|Codex):\*\* [^\n]*(\n[^\n]+)*\n\n`).ReplaceAllString(text, "")
		if strings.Contains(unlabelled, "Codex's sandbox") || strings.Contains(unlabelled, "run_in_background") {
			t.Errorf("%s: a host's instruction outside its label", name)
		}
	}
	watch := shared["watch-task/SKILL.md"]
	for _, want := range []string{
		"**Claude Code:** Run the command below with the shell tool's (Bash or PowerShell)\n`run_in_background` option.\n\n**Codex:** Run the command below as a long-running command",
		"kill the running `wait` command.\n\n**Claude Code:** Use the harness's stop/kill tool for background shells.\n\n**Codex:** Interrupt it, or end its process.\n",
	} {
		if !strings.Contains(watch, want) {
			t.Errorf("watch-task does not contain %q:\n%s", want, watch)
		}
	}
}

// A block that does not start with a paragraph gets its label on a line of
// its own, so a fence or a list is not broken.
func TestSharedLabelBeforeAFence(t *testing.T) {
	in := "Intro.\n<!-- codex -->\n```bash\nx\n```\n<!-- /codex -->\n<!-- claude -->\n- a list\n<!-- /claude -->\nAfter.\n"
	want := "Intro.\n\n**Codex:**\n\n```bash\nx\n```\n\n**Claude Code:**\n\n- a list\n\nAfter.\n"
	if got := shared(in); got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}
