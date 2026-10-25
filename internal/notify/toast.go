package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf16"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// Toast shows a Windows toast with csquad's app ID, through a hidden
// Windows PowerShell (PowerShell 7 cannot load WinRT types). From WSL it is
// the same, through interop.
type Toast struct {
	PowerShell string
	Env        proc.Env
	WSL        bool
}

func (t Toast) Name() string {
	if t.WSL {
		return "Windows toast from WSL (app ID " + AppID + ")"
	}
	return "Windows toast (app ID " + AppID + ")"
}

func (t Toast) Send(ctx context.Context, m Message) error {
	_, err := RunPowerShell(ctx, t.PowerShell, t.Env, ToastScript(m))
	return err
}

// ToastXML is the toast: two text lines in the generic template.
func ToastXML(m Message) string {
	return `<toast><visual><binding template="ToastGeneric"><text>` + xmlText(m.Title) + `</text><text>` + xmlText(m.Body) +
		`</text></binding></visual></toast>`
}

func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// ToastScript shows ToastXML(m). The XML goes in as base64: no quoting
// problem can occur. Windows shows no toast for an app ID that is not
// registered, and no icon for one whose icon file is missing, so the script
// repairs both first: a native and a WSL installation share the app ID,
// and the uninstall of one removes it.
func ToastScript(m Message) string {
	return `$ErrorActionPreference = 'Stop'
$key = ` + psQuote(`HKCU:\`+AppIDKey) + `
$want = Join-Path (Join-Path $env:LOCALAPPDATA 'csquad') 'csquad.png'
if (-not (Test-Path -LiteralPath $key) -or -not (Test-Path -LiteralPath $want) -or (Get-ItemProperty -LiteralPath $key).IconUri -ne $want) {
` + registerBody(PNG(256)) + `}
$null = [Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime]
$null = [Windows.Data.Xml.Dom.XmlDocument, Windows.Data.Xml.Dom.XmlDocument, ContentType = WindowsRuntime]
$doc = New-Object Windows.Data.Xml.Dom.XmlDocument
$doc.LoadXml([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString([]byte(ToastXML(m))) + `')))
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier('` + AppID + `').Show((New-Object Windows.UI.Notifications.ToastNotification $doc))
`
}

// EncodeCommand is a script as powershell -EncodedCommand takes it:
// base64 of UTF-16LE.
func EncodeCommand(script string) string {
	u := utf16.Encode([]rune(script))
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// psPrelude: no progress records (with redirected output PowerShell writes
// them to stderr as CLIXML), and UTF-8 output.
const psPrelude = "$ProgressPreference = 'SilentlyContinue'\n[Console]::OutputEncoding = [Text.Encoding]::UTF8\n"

// RunPowerShell runs a script with a hidden powershell.exe and returns what
// it wrote to stdout.
func RunPowerShell(ctx context.Context, ps string, env proc.Env, script string) (string, error) {
	c := proc.Cmd{Path: ps, Args: []string{"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-EncodedCommand", EncodeCommand(psPrelude + script)}}
	cmd := c.Command(ctx)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	text := strings.TrimSpace(out.String())
	if err != nil {
		if ctx.Err() != nil {
			return text, fmt.Errorf("powershell.exe did not finish in time")
		}
		msg := CLIXMLErrors(errOut.String())
		if msg == "" {
			msg = err.Error()
		}
		return text, fmt.Errorf("powershell.exe: %s", firstLines(msg))
	}
	return text, nil
}

var clixmlError = regexp.MustCompile(`<S S="Error">(.*?)</S>`)

// CLIXMLErrors is the error text of PowerShell's stderr, which is CLIXML
// when the output is redirected.
func CLIXMLErrors(s string) string {
	if !strings.Contains(s, "#< CLIXML") {
		return strings.TrimSpace(s)
	}
	var b strings.Builder
	for _, m := range clixmlError.FindAllStringSubmatch(s, -1) {
		b.WriteString(m[1])
	}
	r := strings.NewReplacer("_x000D_", "", "_x000A_", "\n", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&apos;", "'", "&amp;", "&")
	return strings.TrimSpace(r.Replace(b.String()))
}

func firstLines(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r", ""), "\n")
	if len(lines) > 3 {
		lines = lines[:3]
	}
	return strings.Join(lines, " / ")
}

// WindowsPowerShell finds Windows PowerShell (5.1) on Windows.
func WindowsPowerShell(env proc.Env) string {
	if root := env.Get("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return proc.LookPath("powershell", env, true)
}

// WSLPowerShell finds powershell.exe from WSL: on PATH, else on C:.
func WSLPowerShell(env proc.Env) string {
	if p := proc.LookPath("powershell.exe", env, false); p != "" {
		return p
	}
	p := "/mnt/c/Windows/System32/WindowsPowerShell/v1.0/powershell.exe"
	if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
		return p
	}
	return ""
}

// AppIDKey is the app ID's registry key below HKEY_CURRENT_USER. No admin
// rights are needed.
const AppIDKey = `Software\Classes\AppUserModelId\` + AppID

// Registry is the part of the Windows registry csquad uses: the string
// values of a key below HKEY_CURRENT_USER.
type Registry interface {
	Values(key string) (map[string]string, bool, error)
	SetValues(key string, values map[string]string) error
	DeleteKey(key string) error
}

// Registration is an app ID as install registered it.
type Registration struct {
	Key  string `json:"key"`
	Icon string `json:"icon"` // the IconUri install wrote
}

// Registrar registers csquad's app ID.
type Registrar interface {
	Register(icon []byte) (Registration, error)
	// Registered reads the app ID as it is now.
	Registered() (Registration, bool, error)
	// Unregister removes the key and the icon when the key still names
	// this registration's icon; it reports whether it removed the key.
	Unregister(r Registration) (bool, error)
}

// AppIDIcon is the app ID's icon on Windows: %LOCALAPPDATA%\csquad\csquad.png
// for a native and a WSL installation alike. Windows caches the icon of an
// app ID by the first IconUri it saw, so the path must never change; a
// toast after a change of the path shows no icon.
func AppIDIcon(env proc.Env) string {
	if dir := env.Get("LOCALAPPDATA"); dir != "" {
		return filepath.Join(dir, AppID, "csquad.png")
	}
	return ""
}

// RegistryAppID registers the app ID with the registry API (Windows): the
// icon is a file csquad writes itself.
type RegistryAppID struct {
	Reg  Registry
	Key  string
	Icon string // the icon file
}

func (r RegistryAppID) Register(icon []byte) (Registration, error) {
	if err := writeFile(r.Icon, icon); err != nil {
		return Registration{}, err
	}
	if err := r.Reg.SetValues(r.Key, map[string]string{"DisplayName": AppID, "IconUri": r.Icon}); err != nil {
		return Registration{}, err
	}
	return Registration{Key: r.Key, Icon: r.Icon}, nil
}

func (r RegistryAppID) Registered() (Registration, bool, error) {
	v, ok, err := r.Reg.Values(r.Key)
	if err != nil || !ok {
		return Registration{}, false, err
	}
	return Registration{Key: r.Key, Icon: v["IconUri"]}, true, nil
}

func (r RegistryAppID) Unregister(reg Registration) (bool, error) {
	removed := false
	v, ok, err := r.Reg.Values(reg.Key)
	if err != nil {
		return false, err
	}
	if ok && sameIcon(v["IconUri"], reg.Icon) {
		if err := r.Reg.DeleteKey(reg.Key); err != nil {
			return false, err
		}
		removed = true
	}
	if reg.Icon != "" {
		if err := os.Remove(reg.Icon); err != nil && !errors.Is(err, os.ErrNotExist) {
			return removed, err
		}
		if dir := filepath.Dir(reg.Icon); filepath.Base(dir) == AppID {
			os.Remove(dir) // when empty
		}
	}
	return removed, nil
}

func sameIcon(a, b string) bool { return a != "" && strings.EqualFold(a, b) }

// PowerShellAppID registers the app ID from WSL, through powershell.exe:
// the icon goes to %LOCALAPPDATA%\csquad, where the toast can read it.
type PowerShellAppID struct {
	Run func(script string) (string, error)
	Key string
}

func psQuote(s string) string {
	// PowerShell also ends a single-quoted string at the typographic quotes.
	for _, q := range []string{"'", "‘", "’", "‚", "‛"} {
		s = strings.ReplaceAll(s, q, q+q)
	}
	return "'" + s + "'"
}

func (p PowerShellAppID) psKey() string { return psQuote(`HKCU:\` + p.Key) }

// registerBody writes the icon to %LOCALAPPDATA%\csquad (AppIDIcon) and
// the key named by $key; $icon is then the icon's path.
func registerBody(icon []byte) string {
	return `$dir = Join-Path $env:LOCALAPPDATA 'csquad'
$null = New-Item -ItemType Directory -Force -Path $dir
$icon = Join-Path $dir 'csquad.png'
[IO.File]::WriteAllBytes($icon, [Convert]::FromBase64String('` + base64.StdEncoding.EncodeToString(icon) + `'))
if (-not (Test-Path -LiteralPath $key)) { $null = New-Item -Path $key -Force }
$null = New-ItemProperty -LiteralPath $key -Name DisplayName -Value ` + psQuote(AppID) + ` -PropertyType String -Force
$null = New-ItemProperty -LiteralPath $key -Name IconUri -Value $icon -PropertyType String -Force
`
}

// RegisterScript writes the icon and the key; it prints the icon's path.
func (p PowerShellAppID) RegisterScript(icon []byte) string {
	return `$ErrorActionPreference = 'Stop'
$key = ` + p.psKey() + `
` + registerBody(icon) + `Write-Output $icon
`
}

// QueryScript prints "present", then the key's IconUri, or nothing.
func (p PowerShellAppID) QueryScript() string {
	return `$key = ` + p.psKey() + `
if (Test-Path -LiteralPath $key) { Write-Output 'present'; Write-Output (Get-ItemProperty -LiteralPath $key).IconUri }
`
}

// UnregisterScript removes the key when its IconUri is icon, and the icon.
func (p PowerShellAppID) UnregisterScript(icon string) string {
	return `$ErrorActionPreference = 'Stop'
$key = ` + p.psKey() + `
$icon = ` + psQuote(icon) + `
if ((Test-Path -LiteralPath $key) -and ((Get-ItemProperty -LiteralPath $key).IconUri -eq $icon)) { Remove-Item -LiteralPath $key -Recurse; Write-Output 'removed' }
if ($icon -ne '' -and (Test-Path -LiteralPath $icon)) {
  Remove-Item -LiteralPath $icon
  $dir = Split-Path -Parent $icon
  if (-not (Get-ChildItem -LiteralPath $dir -Force)) { Remove-Item -LiteralPath $dir }
}
`
}

func (p PowerShellAppID) Register(icon []byte) (Registration, error) {
	out, err := p.Run(p.RegisterScript(icon))
	if err != nil {
		return Registration{}, err
	}
	path := lastLine(out)
	if !strings.Contains(path, `:\`) || strings.ContainsAny(path, "<>\"") {
		return Registration{}, fmt.Errorf("powershell.exe printed no icon path: %q", clip(path, 80))
	}
	return Registration{Key: p.Key, Icon: path}, nil
}

func (p PowerShellAppID) Registered() (Registration, bool, error) {
	out, err := p.Run(p.QueryScript())
	if err != nil {
		return Registration{}, false, err
	}
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(out), "\r", ""), "\n")
	if len(lines) == 0 || lines[0] != "present" {
		return Registration{}, false, nil
	}
	r := Registration{Key: p.Key}
	if len(lines) > 1 {
		r.Icon = strings.TrimSpace(lines[1])
	}
	return r, true, nil
}

func (p PowerShellAppID) Unregister(r Registration) (bool, error) {
	out, err := p.Run(p.UnregisterScript(r.Icon))
	return strings.Contains(out, "removed"), err
}

func lastLine(s string) string {
	lines := strings.Split(strings.ReplaceAll(strings.TrimSpace(s), "\r", ""), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func writeFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
