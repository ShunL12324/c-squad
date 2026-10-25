// Package notify shows csquad's native notifications: a toast on Windows
// (and from WSL), UNUserNotificationCenter in a small app bundle on macOS,
// the freedesktop D-Bus service on a Linux desktop. See
// docs/design/notifications.md.
package notify

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// Events, as stored in the tasks table's notified column.
const (
	NeedsInput = "needs_input" // a question, a permission prompt, a held message
	NeedsYou   = "needs_you"   // the phase needs_you
	Ready      = "ready"       // the check passed
	Done       = "done"        // a task without a flow ended its turn
	Failed     = "failed"
	Merged     = "merged" // --merge landed the work
)

// Events lists every event.
var Events = []string{NeedsInput, NeedsYou, Ready, Done, Failed, Merged}

// Modes of CSQUAD_NOTIFY and $CSQUAD_HOME/notify.
const (
	On    = "on"
	Input = "input" // needs input and needs you only
	Off   = "off"
)

// AppID is the Windows app ID (AUMID) and the app name elsewhere.
const AppID = "csquad"

// BundleID is the macOS bundle identifier of csquad.app.
const BundleID = "io.github.shunl12324.csquad"

// ModeFile is where `csquad notify on|input|off` keeps the setting.
func ModeFile(home string) string { return filepath.Join(home, "notify") }

// Mode is the setting: CSQUAD_NOTIFY, else the mode file, else on.
func Mode(env proc.Env, home string) string {
	v := env.Get("CSQUAD_NOTIFY")
	if v == "" {
		b, _ := os.ReadFile(ModeFile(home))
		v = string(b)
	}
	return ParseMode(v)
}

// ParseMode reads a mode; anything unknown is on.
func ParseMode(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case Off, "0", "false", "no", "none":
		return Off
	case Input:
		return Input
	}
	return On
}

// Wants reports whether the mode shows an event.
func Wants(mode, event string) bool {
	switch mode {
	case Off:
		return false
	case Input:
		return event == NeedsInput || event == NeedsYou
	}
	return event != ""
}

// Message is one notification.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// Label is how an event reads in a notification.
func Label(event string) string {
	return strings.ReplaceAll(event, "_", " ")
}

// Text is the notification of an event for a task: the title is the
// task's name (T2 · fix login redirect), the body the state and one detail.
func Text(name, event, detail string) Message {
	body := Label(event)
	if d := FirstLine(detail); d != "" {
		body += " · " + d
	}
	return Message{Title: clip(name, 80), Body: clip(body, 160)}
}

// FirstLine is the first non-empty line of s without markdown heading or
// emphasis marks, its spaces collapsed.
func FirstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		line = strings.TrimSpace(strings.TrimLeft(line, "#"))
		for _, mark := range []string{"> ", "- ", "* "} {
			line = strings.TrimPrefix(line, mark)
		}
		line = strings.Join(strings.Fields(strings.ReplaceAll(line, "**", "")), " ")
		if line != "" {
			return line
		}
	}
	return ""
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

// Backend shows notifications.
type Backend interface {
	// Name says what shows them, e.g. "Windows toast (app ID csquad)".
	Name() string
	Send(ctx context.Context, m Message) error
}

// ErrNone is wrapped by Select when this system has no backend.
var ErrNone = errors.New("no notification backend")

// System is what backend selection looks at.
type System struct {
	OS   string   // runtime.GOOS
	Env  proc.Env // the environment
	Home string   // $CSQUAD_HOME
	// WSL: Linux in the Windows Subsystem for Linux.
	WSL bool
	// Exe is csquad itself (the macOS bundle's binary posts there).
	Exe string
}

// Local is this machine.
func Local(env proc.Env, home string) System {
	exe, _ := os.Executable()
	return System{OS: runtime.GOOS, Env: env, Home: home, WSL: runtime.GOOS == "linux" && IsWSL(env, "/proc/version"), Exe: exe}
}

// IsWSL detects WSL: WSL_DISTRO_NAME, or "microsoft" in /proc/version.
func IsWSL(env proc.Env, procVersion string) bool {
	if env.Get("WSL_DISTRO_NAME") != "" {
		return true
	}
	b, err := os.ReadFile(procVersion)
	return err == nil && strings.Contains(strings.ToLower(string(b)), "microsoft")
}

// Select picks the backend for sys. CSQUAD_NOTIFY_FILE (tests, debugging)
// wins over the OS: notifications are then JSON lines in that file.
func Select(sys System) (Backend, error) {
	if f := sys.Env.Get("CSQUAD_NOTIFY_FILE"); f != "" {
		return File{Path: f}, nil
	}
	switch {
	case sys.OS == "windows":
		ps := WindowsPowerShell(sys.Env)
		if ps == "" {
			return nil, wrapNone("Windows PowerShell (powershell.exe) was not found")
		}
		return Toast{PowerShell: ps, Env: sys.Env}, nil
	case sys.WSL:
		ps := WSLPowerShell(sys.Env)
		if ps == "" {
			return nil, wrapNone("WSL without powershell.exe (Windows interop is off?)")
		}
		return Toast{PowerShell: ps, Env: sys.Env, WSL: true}, nil
	case sys.OS == "darwin":
		app := BundleDir(sys.Home)
		if _, err := os.Stat(BundleExe(app)); err == nil {
			return Bundle{App: app, Env: sys.Env}, nil
		}
		return Osascript{Env: sys.Env}, nil
	case sys.OS == "linux" || sys.OS == "freebsd":
		addr := BusAddress(sys.Env)
		if addr == "" {
			return nil, wrapNone("no D-Bus session bus (DBUS_SESSION_BUS_ADDRESS is unset and $XDG_RUNTIME_DIR/bus is missing)")
		}
		return DBus{Address: addr, Icon: IconFile(sys.Home)}, nil
	}
	return nil, wrapNone("no notification backend for " + sys.OS)
}

type noneError struct{ why string }

func (e noneError) Error() string { return e.why }
func (e noneError) Unwrap() error { return ErrNone }

func wrapNone(why string) error { return noneError{why} }

// BusAddress is the D-Bus session bus: DBUS_SESSION_BUS_ADDRESS, else
// $XDG_RUNTIME_DIR/bus when that socket exists; "" for none. csquad never
// starts a bus (no autolaunch).
func BusAddress(env proc.Env) string {
	if a := env.Get("DBUS_SESSION_BUS_ADDRESS"); a != "" && a != "autolaunch:" {
		return a
	}
	if dir := env.Get("XDG_RUNTIME_DIR"); dir != "" {
		if st, err := os.Stat(filepath.Join(dir, "bus")); err == nil && st.Mode()&os.ModeSocket != 0 {
			return "unix:path=" + filepath.Join(dir, "bus")
		}
	}
	return ""
}

// IconFile is the PNG icon install writes to $CSQUAD_HOME.
func IconFile(home string) string { return filepath.Join(home, "csquad.png") }
