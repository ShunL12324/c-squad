package assets

import (
	"regexp"
	"strings"
	"testing"
)

// Claude Code reads the frontmatter as YAML: an unquoted value must not
// contain ": " or " #", or the skill does not load. Inline code must not wrap
// across lines either.
func TestSkillsHaveValidFrontmatterAndUnbrokenInlineCode(t *testing.T) {
	skills := Skills()
	if len(skills) != 5 {
		t.Fatalf("skills: %v", SkillNames())
	}
	files := map[string]string{"worker.md": WorkerRules(false) + WorkerRules(true)}
	for name, text := range skills {
		files[name] = text
		fm := regexp.MustCompile(`(?s)^---\n(.*?)\n---\n`).FindStringSubmatch(text)
		if fm == nil {
			t.Errorf("%s: no frontmatter", name)
			continue
		}
		keys := map[string]bool{}
		for _, line := range strings.Split(fm[1], "\n") {
			m := regexp.MustCompile(`^([a-z-]+): (.*)$`).FindStringSubmatch(line)
			if m == nil {
				t.Errorf("%s: not a key: value line: %q", name, line)
				continue
			}
			keys[m[1]] = true
			if v := m[2]; !strings.HasPrefix(v, `"`) && !strings.HasPrefix(v, "'") && regexp.MustCompile(`: |\s#`).MatchString(v) {
				t.Errorf("%s: unquoted %s contains \": \" or \" #\"", name, m[1])
			}
		}
		if !keys["name"] || !keys["description"] {
			t.Errorf("%s: name and description required", name)
		}
		if !strings.Contains(text, Placeholder) {
			t.Errorf("%s does not use the binary", name)
		}
		if strings.Contains(text, "tasks.js") || strings.Contains(text, "CSQUAD_DIR") {
			t.Errorf("%s still names the Node implementation", name)
		}
	}
	for name, text := range files {
		fence := false
		for i, line := range strings.Split(text, "\n") {
			if strings.HasPrefix(line, "```") {
				fence = !fence
			} else if !fence && strings.Count(line, "`")%2 != 0 {
				t.Errorf("%s:%d: inline code wraps: %s", name, i+1, line)
			}
		}
	}
}

func TestWorkerRulesBlocks(t *testing.T) {
	wt, ip := WorkerRules(false), WorkerRules(true)
	if !strings.Contains(wt, "Prefer an isolated git") || strings.Contains(wt, "launched to work in place") || strings.Contains(wt, "<!--") {
		t.Error("worktree rules")
	}
	if !strings.Contains(ip, "launched to work in place") || strings.Contains(ip, "Prefer an isolated git") || strings.Contains(ip, "Commit finished code") || strings.Contains(ip, "<!--") {
		t.Error("in-place rules")
	}
}

func TestRenderQuotesThePath(t *testing.T) {
	text := Placeholder + " status"
	for _, c := range []struct {
		bin  string
		win  bool
		want string
	}{
		{"/home/u/.csquad/bin/csquad", false, "/home/u/.csquad/bin/csquad status"},
		{"/home/a b/.csquad/bin/csquad", false, "'/home/a b/.csquad/bin/csquad' status"},
		{"/home/it's/csquad", false, `'/home/it'\''s/csquad' status`},
		{`C:\Users\shun\.csquad\bin\csquad.exe`, true, "C:/Users/shun/.csquad/bin/csquad.exe status"},
		{`C:\Users\First Last\.csquad\bin\csquad.exe`, true, `"C:/Users/First Last/.csquad/bin/csquad.exe" status`},
	} {
		if got := Render(text, c.bin, c.win); got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.bin, got, c.want)
		}
	}
}
