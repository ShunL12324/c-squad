package notify

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"image/png"
	"maps"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/ShunL12324/c-squad/internal/proc"
)

func TestText(t *testing.T) {
	cases := []struct{ event, detail, body string }{
		{NeedsInput, "## May I run the migration?\nIt drops a table.", "needs input · May I run the migration?"},
		{NeedsYou, "merge conflict: worktree-t2-x still does not rebase onto main; csquad retry tries again", "needs you · merge conflict: worktree-t2-x still does not rebase onto main; csquad retry tries again"},
		{Done, "\n\n- **Fixed** the  redirect\n", "done · Fixed the redirect"},
		{Ready, "", "ready"},
		{Failed, "", "failed"},
		{Merged, "into main", "merged · into main"},
	}
	for _, c := range cases {
		if m := Text("T2 · fix login redirect", c.event, c.detail); m.Title != "T2 · fix login redirect" || m.Body != c.body {
			t.Errorf("%s: %+v, want body %q", c.event, m, c.body)
		}
	}
	long := Text(strings.Repeat("t", 200), Done, strings.Repeat("word ", 100))
	if n := len([]rune(long.Body)); n != 160 || !strings.HasSuffix(long.Body, "…") || len([]rune(long.Title)) != 80 {
		t.Errorf("not clipped: %d %q", n, long.Body)
	}
}

func TestModes(t *testing.T) {
	home := t.TempDir()
	if m := Mode(nil, home); m != On {
		t.Errorf("default %s", m)
	}
	os.WriteFile(ModeFile(home), []byte("input\n"), 0o644)
	if m := Mode(nil, home); m != Input {
		t.Errorf("file %s", m)
	}
	if m := Mode(proc.Env{"CSQUAD_NOTIFY=off"}, home); m != Off {
		t.Errorf("the variable wins: %s", m)
	}
	for mode, want := range map[string][]string{
		On:    Events,
		Input: {NeedsInput, NeedsYou},
		Off:   nil,
	} {
		var got []string
		for _, e := range Events {
			if Wants(mode, e) {
				got = append(got, e)
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("%s: %v", mode, got)
		}
		if Wants(mode, "") {
			t.Errorf("%s wants a quiet state", mode)
		}
	}
}

func exe(t *testing.T, path string) string {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSelect(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "csquad")

	// WSL: the Windows toast through powershell.exe on PATH.
	winBin := filepath.Join(root, "win")
	ps := exe(t, filepath.Join(winBin, "powershell.exe"))
	// (Windows has no executable bit for a Linux PATH lookup to see.)
	if runtime.GOOS != "windows" {
		b, err := Select(System{OS: "linux", WSL: true, Home: home, Env: proc.Env{"PATH=" + winBin}})
		if tb, ok := b.(Toast); err != nil || !ok || !tb.WSL || tb.PowerShell != ps || b.Name() != "Windows toast from WSL (app ID csquad)" {
			t.Errorf("WSL: %#v %v", b, err)
		}
	}
	// Windows: Windows PowerShell 5.1, not pwsh.
	sysRoot := filepath.Join(root, "Windows")
	ps51 := exe(t, filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
	b, err := Select(System{OS: "windows", Home: home, Env: proc.Env{"SystemRoot=" + sysRoot}})
	if tb, ok := b.(Toast); err != nil || !ok || tb.WSL || tb.PowerShell != ps51 {
		t.Errorf("Windows: %#v %v", b, err)
	}
	// Linux without a session bus: none, and why.
	_, err = Select(System{OS: "linux", Home: home, Env: proc.Env{"XDG_RUNTIME_DIR=" + root}})
	if !errors.Is(err, ErrNone) || !strings.Contains(err.Error(), "no D-Bus session bus") {
		t.Errorf("no bus: %v", err)
	}
	// Linux with one: the variable, else $XDG_RUNTIME_DIR/bus.
	b, err = Select(System{OS: "linux", Home: home, Env: proc.Env{"DBUS_SESSION_BUS_ADDRESS=unix:path=/run/user/1/bus"}})
	if d, ok := b.(DBus); err != nil || !ok || d.Address != "unix:path=/run/user/1/bus" || d.Icon != IconFile(home) {
		t.Errorf("bus: %#v %v", b, err)
	}
	if runtime.GOOS != "windows" {
		run := filepath.Join(root, "run")
		os.MkdirAll(run, 0o700)
		l, err := net.Listen("unix", filepath.Join(run, "bus"))
		if err != nil {
			t.Fatal(err)
		}
		defer l.Close()
		if a := BusAddress(proc.Env{"XDG_RUNTIME_DIR=" + run}); a != "unix:path="+filepath.Join(run, "bus") {
			t.Errorf("XDG_RUNTIME_DIR/bus: %q", a)
		}
	}
	// macOS: csquad.app when install made it, else osascript.
	if b, err := Select(System{OS: "darwin", Home: home}); err != nil || b.Name() != (Osascript{}).Name() {
		t.Errorf("macOS without csquad.app: %#v %v", b, err)
	}
	exe(t, BundleExe(BundleDir(home)))
	if b, err := Select(System{OS: "darwin", Home: home}); err != nil || b.(Bundle).App != BundleDir(home) {
		t.Errorf("macOS: %#v %v", b, err)
	}
	// CSQUAD_NOTIFY_FILE wins everywhere.
	for _, os := range []string{"linux", "windows", "darwin"} {
		b, err := Select(System{OS: os, WSL: os == "linux", Home: home, Env: proc.Env{"CSQUAD_NOTIFY_FILE=" + filepath.Join(root, "n.jsonl")}})
		if _, ok := b.(File); err != nil || !ok {
			t.Errorf("%s with CSQUAD_NOTIFY_FILE: %#v %v", os, b, err)
		}
	}
}

func TestIsWSL(t *testing.T) {
	f := filepath.Join(t.TempDir(), "version")
	os.WriteFile(f, []byte("Linux version 6.6.87.2-microsoft-standard-WSL2 (root@x)"), 0o644)
	if !IsWSL(nil, f) {
		t.Error("/proc/version of WSL")
	}
	os.WriteFile(f, []byte("Linux version 6.8.0-45-generic (buildd@lcy02)"), 0o644)
	if IsWSL(nil, f) || !IsWSL(proc.Env{"WSL_DISTRO_NAME=Ubuntu"}, f) {
		t.Error("WSL_DISTRO_NAME decides on a plain kernel")
	}
}

func decodeUTF16(t *testing.T, b64 string) string {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		t.Fatal(err)
	}
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = binary.LittleEndian.Uint16(b[2*i:])
	}
	return string(utf16.Decode(u))
}

func TestToast(t *testing.T) {
	m := Message{Title: `T2 · <fix> & "go"`, Body: "needs input · it's ‘quoted’"}
	x := ToastXML(m)
	if x != `<toast><visual><binding template="ToastGeneric"><text>T2 · &lt;fix&gt; &amp; &#34;go&#34;</text><text>needs input · it&#39;s ‘quoted’</text></binding></visual></toast>` {
		t.Errorf("xml %s", x)
	}
	script := ToastScript(m)
	if !strings.Contains(script, "CreateToastNotifier('csquad')") || strings.Contains(script, "fix") {
		t.Errorf("the texts must go in as base64:\n%s", script)
	}
	b64 := script[strings.Index(script, "FromBase64String('")+18:]
	b64 = b64[:strings.Index(b64, "'")]
	if b, _ := base64.StdEncoding.DecodeString(b64); string(b) != x {
		t.Errorf("the script holds %s", b)
	}
	if got := decodeUTF16(t, EncodeCommand(script)); got != script {
		t.Errorf("EncodeCommand does not round-trip")
	}
}

type fakeRegistry map[string]map[string]string

func (f fakeRegistry) Values(key string) (map[string]string, bool, error) {
	v, ok := f[key]
	return v, ok, nil
}

func (f fakeRegistry) SetValues(key string, values map[string]string) error {
	if f[key] == nil {
		f[key] = map[string]string{}
	}
	maps.Copy(f[key], values)
	return nil
}

func (f fakeRegistry) DeleteKey(key string) error { delete(f, key); return nil }

func TestRegistryAppID(t *testing.T) {
	testRegistryAppID(t, fakeRegistry{}, AppIDKey)
}

func testRegistryAppID(t *testing.T, reg Registry, key string) {
	t.Helper()
	icon := filepath.Join(t.TempDir(), "csquad.png")
	r := RegistryAppID{Reg: reg, Key: key, Icon: icon}
	got, err := r.Register(PNG(64))
	if err != nil || got != (Registration{Key: key, Icon: icon}) {
		t.Fatalf("%+v %v", got, err)
	}
	if v, ok, _ := reg.Values(key); !ok || v["DisplayName"] != "csquad" || v["IconUri"] != icon {
		t.Errorf("values %v", v)
	}
	if _, err := os.Stat(icon); err != nil {
		t.Error("no icon file")
	}
	if now, ok, err := r.Registered(); !ok || err != nil || now.Icon != icon {
		t.Errorf("registered %+v %v %v", now, ok, err)
	}
	// Another installation registered since: its key stays, our icon goes.
	reg.SetValues(key, map[string]string{"IconUri": `C:\Users\u\AppData\Local\csquad\csquad.png`})
	if removed, err := r.Unregister(got); removed || err != nil {
		t.Errorf("removed another installation's key: %v %v", removed, err)
	}
	if _, ok, _ := reg.Values(key); !ok {
		t.Error("the key is gone")
	}
	if _, err := os.Stat(icon); !errors.Is(err, os.ErrNotExist) {
		t.Error("the icon is left")
	}
	r.Register(PNG(64))
	if removed, err := r.Unregister(got); !removed || err != nil {
		t.Errorf("unregister %v %v", removed, err)
	}
	if _, ok, _ := reg.Values(key); ok {
		t.Error("the key is left")
	}
	if removed, err := r.Unregister(got); removed || err != nil {
		t.Errorf("twice: %v %v", removed, err)
	}
}

func TestPowerShellAppID(t *testing.T) {
	var scripts []string
	out := ""
	p := PowerShellAppID{Key: AppIDKey, Run: func(s string) (string, error) { scripts = append(scripts, s); return out, nil }}
	out = "\r\nC:\\Users\\u\\AppData\\Local\\csquad\\csquad.png\r\n"
	reg, err := p.Register(PNG(32))
	if err != nil || reg.Icon != `C:\Users\u\AppData\Local\csquad\csquad.png` || reg.Key != AppIDKey {
		t.Fatalf("%+v %v", reg, err)
	}
	s := scripts[0]
	for _, want := range []string{`$key = 'HKCU:\Software\Classes\AppUserModelId\csquad'`, "-Name DisplayName -Value 'csquad'", "-Name IconUri -Value $icon", "$env:LOCALAPPDATA", base64.StdEncoding.EncodeToString(PNG(32))} {
		if !strings.Contains(s, want) {
			t.Errorf("register script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "New-Item -Path $key -Force }") && !strings.Contains(s, "if (-not (Test-Path -LiteralPath $key))") {
		t.Error("New-Item -Force on an existing key would drop its values")
	}
	out = "present\r\nC:\\x.png\r\n"
	if r, ok, err := p.Registered(); !ok || err != nil || r.Icon != `C:\x.png` {
		t.Errorf("registered %+v %v %v", r, ok, err)
	}
	out = ""
	if _, ok, _ := p.Registered(); ok {
		t.Error("an empty answer is no key")
	}
	out = "removed\r\n"
	if removed, err := p.Unregister(Registration{Key: AppIDKey, Icon: `C:\it's.png`}); !removed || err != nil {
		t.Errorf("unregister %v %v", removed, err)
	}
	if last := scripts[len(scripts)-1]; !strings.Contains(last, `$icon = 'C:\it''s.png'`) || !strings.Contains(last, "-eq $icon") {
		t.Errorf("unregister script:\n%s", last)
	}
	if q := psQuote("a’b"); q != "'a’’b'" {
		t.Errorf("psQuote %s", q)
	}
}

func TestIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(PNG(256)))
	if err != nil || img.Bounds().Dx() != 256 || img.Bounds().Dy() != 256 {
		t.Fatalf("%v %v", img.Bounds(), err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Error("the corner is not round")
	}
	if r, g, b, _ := img.At(128, 2).RGBA(); r>>8 != 0x1e || g>>8 != 0x1e || b>>8 != 0x2e {
		t.Errorf("background %x %x %x", r>>8, g>>8, b>>8)
	}
	// The top-left block of the ">": mint.
	scale := 0.72 * 256 / logoW
	x, y := int((256-logoW*scale)/2+12*scale), int((256-logoH*scale)/2+12*scale)
	if r, g, b, _ := img.At(x, y).RGBA(); r>>8 != 0x78 || g>>8 != 0xe8 || b>>8 != 0xaf {
		t.Errorf("logo %x %x %x at %d,%d", r>>8, g>>8, b>>8, x, y)
	}
	icns := ICNS()
	if string(icns[:4]) != "icns" || int(binary.BigEndian.Uint32(icns[4:8])) != len(icns) {
		t.Fatalf("icns header %q", icns[:8])
	}
	var kinds []string
	for off := 8; off < len(icns); {
		kind, n := string(icns[off:off+4]), int(binary.BigEndian.Uint32(icns[off+4:off+8]))
		if _, err := png.Decode(bytes.NewReader(icns[off+8 : off+n])); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
		kinds = append(kinds, kind)
		off += n
	}
	if strings.Join(kinds, ",") != "ic07,ic08,ic09" {
		t.Errorf("icns entries %v", kinds)
	}
}

func TestWriteBundle(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "csquad")
	os.WriteFile(bin, []byte("binary"), 0o755)
	app := BundleDir(filepath.Join(root, "home"))
	if err := WriteBundle(app, bin, "3.1.0"); err != nil {
		t.Fatal(err)
	}
	plist, err := os.ReadFile(filepath.Join(app, "Contents", "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{"CFBundleIdentifier": BundleID, "CFBundleName": "csquad", "CFBundleExecutable": "csquad",
		"CFBundleIconFile": "csquad", "CFBundlePackageType": "APPL", "CFBundleVersion": "3.1.0", "LSUIElement": "true"} {
		if got, ok := PlistString(plist, key); !ok || got != want {
			t.Errorf("%s = %q, want %q", key, got, want)
		}
	}
	if b, err := os.ReadFile(BundleExe(app)); err != nil || string(b) != "binary" {
		t.Errorf("the binary: %q %v", b, err)
	}
	if st, _ := os.Stat(BundleExe(app)); runtime.GOOS != "windows" && st.Mode()&0o111 == 0 {
		t.Error("the bundle's csquad is not executable")
	}
	if icns, _ := os.ReadFile(filepath.Join(app, "Contents", "Resources", "csquad.icns")); !bytes.Equal(icns, ICNS()) {
		t.Error("the icon")
	}
	entries, _ := os.ReadDir(filepath.Join(app, "Contents"))
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if strings.Join(names, ",") != "Info.plist,MacOS,Resources" {
		t.Errorf("Contents holds %v", names)
	}
}

type fakeRegistrar struct {
	reg        *Registration
	registered int
	removed    []Registration
}

func (f *fakeRegistrar) Register(icon []byte) (Registration, error) {
	f.registered++
	r := Registration{Key: AppIDKey, Icon: `C:\icon.png`}
	f.reg = &r
	return r, nil
}

func (f *fakeRegistrar) Registered() (Registration, bool, error) {
	if f.reg == nil {
		return Registration{}, false, nil
	}
	return *f.reg, true, nil
}

func (f *fakeRegistrar) Unregister(r Registration) (bool, error) {
	f.removed = append(f.removed, r)
	return true, nil
}

func TestSetup(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "csquad-bin")
	os.WriteFile(bin, []byte("binary"), 0o755)
	winBin := filepath.Join(root, "win")
	exe(t, filepath.Join(winBin, "powershell.exe"))
	sysRoot := filepath.Join(root, "Windows")
	exe(t, filepath.Join(sysRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))

	t.Run("macOS", func(t *testing.T) {
		home := filepath.Join(root, "mac")
		var calls []string
		s := Setup{Sys: System{OS: "darwin", Home: home}, Version: "3.1.0", Binary: bin,
			Exec: func(name string, args ...string) error {
				calls = append(calls, filepath.Base(name)+" "+strings.Join(args, " "))
				return nil
			}}
		m, err := s.Install()
		app := BundleDir(home)
		if err != nil || len(m.Files) != 1 || m.Files[0] != app || m.Backend != "macOS notification ("+app+")" || m.AppID != nil {
			t.Fatalf("%+v %v", m, err)
		}
		if strings.Join(calls, "; ") != "codesign --force --sign - "+app+"; lsregister -f "+app {
			t.Errorf("calls %v", calls)
		}
		calls = nil
		if notes := s.Uninstall(m); len(notes) != 0 {
			t.Errorf("notes %v", notes)
		}
		if _, err := os.Stat(app); !errors.Is(err, os.ErrNotExist) || strings.Join(calls, "; ") != "lsregister -u "+app {
			t.Errorf("csquad.app is left (%v), calls %v", err, calls)
		}
	})
	t.Run("Windows", func(t *testing.T) {
		home := filepath.Join(root, "win-home")
		r := &fakeRegistrar{}
		s := Setup{Sys: System{OS: "windows", Home: home, Env: proc.Env{"SystemRoot=" + sysRoot}}, Registrar: r}
		m, err := s.Install()
		if err != nil || r.registered != 1 || m.AppID == nil || m.WSL || m.Backend != "Windows toast (app ID csquad)" || len(m.Files) != 1 || m.Files[0] != IconFile(home) {
			t.Fatalf("%+v %v", m, err)
		}
		s.Uninstall(m)
		if len(r.removed) != 1 || r.removed[0] != *m.AppID {
			t.Errorf("unregistered %v", r.removed)
		}
	})
	t.Run("WSL", func(t *testing.T) {
		home := filepath.Join(root, "wsl-home")
		r := &fakeRegistrar{}
		s := Setup{Sys: System{OS: "linux", WSL: true, Home: home, Env: proc.Env{"PATH=" + winBin}}, Registrar: r}
		m, err := s.Install()
		if err != nil || r.registered != 1 || !m.WSL || len(m.Files) != 0 || runtime.GOOS != "windows" && m.Backend != "Windows toast from WSL (app ID csquad)" {
			t.Fatalf("%+v %v", m, err)
		}
		s.Uninstall(m)
		if len(r.removed) != 1 {
			t.Errorf("unregistered %v", r.removed)
		}
	})
	t.Run("Linux", func(t *testing.T) {
		home := filepath.Join(root, "linux-home")
		s := Setup{Sys: System{OS: "linux", Home: home}}
		m, err := s.Install()
		if err != nil || len(m.Files) != 1 || !strings.HasPrefix(m.Backend, "none (no D-Bus session bus") {
			t.Fatalf("%+v %v", m, err)
		}
		if b, _ := os.ReadFile(IconFile(home)); !bytes.Equal(b, PNG(256)) {
			t.Error("the icon")
		}
		// Uninstall never removes a file outside $CSQUAD_HOME.
		outside := filepath.Join(root, "keep.txt")
		os.WriteFile(outside, nil, 0o644)
		m.Files = append(m.Files, outside, home)
		s.Uninstall(m)
		if _, err := os.Stat(outside); err != nil {
			t.Error("removed a file outside $CSQUAD_HOME")
		}
		if _, err := os.Stat(IconFile(home)); !errors.Is(err, os.ErrNotExist) {
			t.Error("the icon is left")
		}
		if _, err := os.Stat(home); err != nil {
			t.Error("removed $CSQUAD_HOME itself")
		}
	})
	t.Run("file", func(t *testing.T) {
		home := filepath.Join(root, "file-home")
		r := &fakeRegistrar{}
		s := Setup{Sys: System{OS: "windows", Home: home, Env: proc.Env{"CSQUAD_NOTIFY_FILE=" + filepath.Join(root, "n.jsonl")}}, Registrar: r}
		m, err := s.Install()
		if err != nil || r.registered != 0 || len(m.Files) != 0 || !strings.HasPrefix(m.Backend, "file ") {
			t.Fatalf("%+v %v", m, err)
		}
		if _, err := os.Stat(home); !errors.Is(err, os.ErrNotExist) {
			t.Error("set something up with CSQUAD_NOTIFY_FILE")
		}
	})
}

func TestFileBackend(t *testing.T) {
	f := File{Path: filepath.Join(t.TempDir(), "d", "n.jsonl")}
	f.Send(context.Background(), Message{Title: "a", Body: "b"})
	f.Send(context.Background(), Message{Title: "c", Body: "d"})
	if b, _ := os.ReadFile(f.Path); string(b) != "{\"title\":\"a\",\"body\":\"b\"}\n{\"title\":\"c\",\"body\":\"d\"}\n" {
		t.Errorf("%s", b)
	}
}
