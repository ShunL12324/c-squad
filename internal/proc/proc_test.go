package proc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Windows resolution is pure apart from the files it looks at, so it runs
// on every OS with win passed in.

var npmShim = []string{
	"@ECHO off", "GOTO start", ":find_dp0", "SET dp0=%~dp0", "EXIT /b", ":start", "SETLOCAL", "CALL :find_dp0", "",
	`IF EXIST "%dp0%\node.exe" (`, `  SET "_prog=%dp0%\node.exe"`, ") ELSE (", `  SET "_prog=node"`, ")", "",
	`endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & "%_prog%"  "%dp0%\node_modules\claude\cli.js" %*`,
}

func write(t *testing.T, path, content string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestShimResolvesToItsNodeScript(t *testing.T) {
	bin := t.TempDir()
	write(t, filepath.Join(bin, "node_modules", "claude", "cli.js"), "")
	write(t, filepath.Join(bin, "claude.cmd"), strings.Join(npmShim, "\r\n")+"\r\n")
	nodeDir := t.TempDir()
	write(t, filepath.Join(nodeDir, "node.exe"), "")
	env := Env{"PATH=" + bin + ";" + nodeDir, "PATHEXT=.COM;.EXE;.BAT;.CMD"}
	args := []string{"multi\nline \"prompt\"\r\nwith 100% ^ & |", strings.Repeat("x", 20000)}
	c, err := resolve("claude", args, env, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != filepath.Join(nodeDir, "node.exe") || c.Args[0] != filepath.Join(bin, "node_modules", "claude", "cli.js") || c.CmdLine != "" {
		t.Errorf("%+v", c)
	}
	if strings.Join(c.Args[1:], "\x00") != strings.Join(args, "\x00") {
		t.Error("arguments changed")
	}
}

func TestExePreferredInPathOrder(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	write(t, filepath.Join(a, "claude.exe"), "")
	write(t, filepath.Join(b, "claude.cmd"), "")
	env := Env{"Path=" + a + ";" + b, "PATHEXT=.COM;.EXE;.BAT;.CMD"} // Windows spells it Path
	c, _ := resolve("claude", []string{"agents"}, env, true)
	if c.Path != filepath.Join(a, "claude.exe") || c.Args[0] != "agents" {
		t.Errorf("%+v", c)
	}
	if c, _ := resolve("claude", nil, Env{"PATH=" + t.TempDir()}, true); c.Path != "claude" {
		t.Errorf("not found: %+v", c)
	}
}

func TestUnreadableShimGoesThroughCmdExe(t *testing.T) {
	bin := t.TempDir()
	write(t, filepath.Join(bin, "claude.cmd"), "@ECHO off\r\nSET \"F=%~dp0cli.js\"\r\nnode \"%F%\" %*\r\n")
	env := Env{"PATH=" + bin, "PATHEXT=.CMD", `ComSpec=C:\Windows\system32\cmd.exe`}
	c, err := resolve("claude", []string{"two words", `say "hi"`, "a&b"}, env, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Path != `C:\Windows\system32\cmd.exe` || !strings.HasPrefix(c.CmdLine, `"C:\Windows\system32\cmd.exe" /d /s /c "`) {
		t.Errorf("%+v", c)
	}
	if !strings.Contains(c.CmdLine, `^^^"two^^^ words^^^"`) || !strings.Contains(c.CmdLine, `a^^^&b`) {
		t.Errorf("quoting: %s", c.CmdLine)
	}
	if _, err := resolve("claude", []string{"a\nb"}, env, true); err == nil || !strings.Contains(err.Error(), "multi-line") {
		t.Errorf("newline: %v", err)
	}
	if _, err := resolve("claude", []string{strings.Repeat("y", 9000)}, env, true); err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("long: %v", err)
	}
}

func TestCmdQuote(t *testing.T) {
	for _, c := range []struct{ in, once, twice string }{
		{"plain", `^"plain^"`, `^^^"plain^^^"`},
		{`back\slash\`, `^"back\slash\\^"`, `^^^"back\slash\\^^^"`},
		{`say "hi"`, `^"say^ \^"hi\^"^"`, `^^^"say^^^ \^^^"hi\^^^"^^^"`},
		{"100%", `^"100^%^"`, `^^^"100^^^%^^^"`},
	} {
		if got := CmdQuote(c.in, false); got != c.once {
			t.Errorf("CmdQuote(%q) = %s, want %s", c.in, got, c.once)
		}
		if got := CmdQuote(c.in, true); got != c.twice {
			t.Errorf("CmdQuote(%q, twice) = %s, want %s", c.in, got, c.twice)
		}
	}
}

func TestEnvIsCaseInsensitiveOnWindowsOnly(t *testing.T) {
	e := Env{"Path=a", "HOME=h"}
	if e.get("PATH", true) != "a" || e.get("PATH", false) != "" || e.get("HOME", false) != "h" {
		t.Error("get")
	}
}

func TestGitBash(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "Git", "cmd", "git.exe"), "")
	write(t, filepath.Join(root, "Git", "bin", "bash.exe"), "")
	env := Env{"PATH=" + filepath.Join(root, "Git", "cmd"), "PATHEXT=.EXE"}
	if got := GitBash(env); got != filepath.Join(root, "Git", "cmd", "..", "bin", "bash.exe") {
		t.Errorf("%q", got)
	}
	if got := GitBash(Env{"ProgramFiles=" + root}); got != filepath.Join(root, "Git", "bin", "bash.exe") {
		t.Errorf("%q", got)
	}
}

func TestPowerShell(t *testing.T) {
	root := t.TempDir()
	windowsPS := filepath.Join(root, "Windows", "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	write(t, windowsPS, "")
	env := Env{"SystemRoot=" + filepath.Join(root, "Windows"), "ProgramFiles=" + filepath.Join(root, "Program Files")}
	if got := PowerShell(env); got != windowsPS {
		t.Errorf("%q", got)
	}
	pwsh := filepath.Join(root, "Program Files", "PowerShell", "7", "pwsh.exe")
	write(t, pwsh, "")
	if got := PowerShell(env); got != pwsh {
		t.Errorf("PowerShell 7 first: %q", got)
	}
	if got := PowerShell(Env{}); got != "" {
		t.Errorf("%q", got)
	}
}

func TestTildeHome(t *testing.T) {
	for _, c := range []struct {
		env  Env
		want string
	}{
		{Env{`USERPROFILE=C:\Users\First Last`}, `C:\Users\First Last`},
		{Env{`USERPROFILE=C:\Users\First Last`, `HOME=C:\Users\First Last`}, `C:\Users\First Last`},
		{Env{`UserProfile=C:\Users\First Last`, `home=c:\users\first last\`}, `C:\Users\First Last`},
		{Env{`USERPROFILE=C:\Users\First Last`, `HOME=D:\home`}, ""},
		{Env{`HOME=/home/u`}, ""},
	} {
		if got := c.env.TildeHome(); got != c.want {
			t.Errorf("%v: %q", c.env, got)
		}
	}
}
