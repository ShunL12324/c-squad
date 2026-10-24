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
	if len(Skills()) != 5 {
		t.Fatalf("skills: %v", SkillNames())
	}
	files := map[string]string{"worker.md": WorkerRules(false) + WorkerRules(true),
		"worker-codex.md": WorkerRulesFor("codex", false) + WorkerRulesFor("codex", true), "flow.md": FlowRules()}
	skills := map[string]string{}
	for _, host := range Hosts {
		for name, text := range SkillsFor(host) {
			skills[host+"/"+name] = text
		}
		for name, text := range SkillFilesFor(host) {
			if !strings.HasSuffix(name, "/SKILL.md") {
				files[host+"/"+name] = text
			}
		}
	}
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
		if got := Render(text, c.bin, c.win, ""); got != c.want {
			t.Errorf("Render(%q) = %q, want %q", c.bin, got, c.want)
		}
	}
}

// A Windows program path that needs quotes goes below ~ instead, where it
// can: PowerShell cannot start a quoted program without &.
func TestProgramPath(t *testing.T) {
	for _, c := range []struct {
		p, home string
		win     bool
		want    string
	}{
		{`C:\Users\First Last\.csquad\bin\csquad.exe`, `C:\Users\First Last`, true, "~/.csquad/bin/csquad.exe"},
		{`C:\Users\First Last\.csquad\bin\csquad.exe`, `c:/users/first last/`, true, "~/.csquad/bin/csquad.exe"},
		{`C:\Users\shun\.csquad\bin\csquad.exe`, `C:\Users\shun`, true, "C:/Users/shun/.csquad/bin/csquad.exe"},
		{`C:\Users\First Last\my csquad\csquad.exe`, `C:\Users\First Last`, true, `"C:/Users/First Last/my csquad/csquad.exe"`},
		{`D:\First Last\csquad.exe`, `C:\Users\First Last`, true, `"D:/First Last/csquad.exe"`},
		{`C:\Users\First Lastname\csquad.exe`, `C:\Users\First Last`, true, `"C:/Users/First Lastname/csquad.exe"`},
		{`C:\Users\First Last\.csquad\bin\csquad.exe`, "", true, `"C:/Users/First Last/.csquad/bin/csquad.exe"`},
		{"/home/a b/.csquad/bin/csquad", "/home/a b", false, "'/home/a b/.csquad/bin/csquad'"},
	} {
		if got := ProgramPath(c.p, c.win, c.home); got != c.want {
			t.Errorf("ProgramPath(%q, %q) = %q, want %q", c.p, c.home, got, c.want)
		}
	}
}

func TestSkillsPerHost(t *testing.T) {
	claude, codex := SkillsFor("claude"), SkillsFor("codex")
	for name := range Skills() {
		for host, text := range map[string]string{"claude": claude[name], "codex": codex[name]} {
			if strings.Contains(text, "<!--") {
				t.Errorf("%s/%s keeps a block marker", host, name)
			}
		}
		if !strings.Contains(codex[name], "outside Codex's sandbox") {
			t.Errorf("codex/%s does not say to run csquad outside the sandbox", name)
		}
		if strings.Contains(claude[name], "Codex's sandbox") {
			t.Errorf("claude/%s carries the Codex sandbox note", name)
		}
	}
	for _, host := range Hosts {
		for name, text := range SkillFilesFor(host) {
			// One way to send, one launch form: no SendMessage, no draft files.
			if strings.Contains(text, "SendMessage") || strings.Contains(text, "--prompt-file") {
				t.Errorf("%s/%s names SendMessage or --prompt-file", host, name)
			}
		}
		create := SkillFilesFor(host)["create-task/SKILL.md"]
		if !strings.Contains(create, "<<'PROMPT'") || !strings.Contains(create, "here-string") || !strings.Contains(create, "Before every launch, read `~/.csquad/models.md`") {
			t.Errorf("%s/create-task: the prompt on stdin, models.md at every dispatch", host)
		}
	}
}

// create-task is a short core; the rest is in reference.md, read on demand.
func TestCreateTaskIsACorePlusReference(t *testing.T) {
	files := SkillFiles()
	core, ref := files["create-task/SKILL.md"], files["create-task/reference.md"]
	if ref == "" || !strings.Contains(core, "`reference.md` next to this file") {
		t.Fatal("create-task/reference.md missing or not named by the core")
	}
	if words := len(strings.Fields(regexp.MustCompile(`(?s)^---\n.*?\n---\n`).ReplaceAllString(core, ""))); words > 470 {
		t.Errorf("the create-task core has %d words", words)
	}
	for _, flag := range []string{"--after", "--check", "--merge", "--isolation none", "--fork", "--no-worker-rules", "-c/--config"} {
		if !strings.Contains(ref, flag) {
			t.Errorf("reference.md does not explain %s", flag)
		}
	}
	for _, host := range Hosts {
		if strings.Contains(SkillFilesFor(host)["create-task/reference.md"], "<!--") {
			t.Errorf("%s/reference.md keeps a block marker", host)
		}
	}
}

func TestCodexWorkerRules(t *testing.T) {
	wt, ip := WorkerRulesFor("codex", false), WorkerRulesFor("codex", true)
	if !strings.Contains(wt, "-C REPO worktree add ../NAME-wt/t12-retry-sqlite-busy -b task/t12-retry-sqlite-busy") || strings.Contains(wt, "launched to work in place") || strings.Contains(wt, "<!--") {
		t.Errorf("worktree rules:\n%s", wt)
	}
	if !strings.Contains(ip, "launched to work in place") || strings.Contains(ip, "worktree add") || strings.Contains(ip, "<!--") {
		t.Errorf("in-place rules:\n%s", ip)
	}
	for _, r := range []string{wt, ip} {
		if !strings.Contains(r, "ending with `?`") || strings.Contains(r, "Claude Code") {
			t.Error("Codex rules: end with the question, nothing about Claude Code")
		}
	}
	if WorkerRulesFor("claude", false) != WorkerRules(false) {
		t.Error("claude rules")
	}
}
