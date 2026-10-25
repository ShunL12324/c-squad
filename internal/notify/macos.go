package notify

import (
	"bytes"
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// BundleDir is csquad.app in $CSQUAD_HOME: only an app bundle can use
// UNUserNotificationCenter, and the notification shows its name and icon.
func BundleDir(home string) string { return filepath.Join(home, "csquad.app") }

// BundleExe is the bundle's copy of csquad.
func BundleExe(app string) string { return filepath.Join(app, "Contents", "MacOS", "csquad") }

// Lsregister registers a bundle with LaunchServices, which the
// notification service asks for the app.
const Lsregister = "/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"

// InfoPlist is the bundle's Info.plist: an agent app (LSUIElement) with no
// Dock icon and no menu bar.
func InfoPlist(version string) []byte {
	keys := [][2]string{
		{"CFBundleDevelopmentRegion", "en"},
		{"CFBundleDisplayName", AppID},
		{"CFBundleExecutable", "csquad"},
		{"CFBundleIconFile", "csquad"},
		{"CFBundleIdentifier", BundleID},
		{"CFBundleInfoDictionaryVersion", "6.0"},
		{"CFBundleName", AppID},
		{"CFBundlePackageType", "APPL"},
		{"CFBundleShortVersionString", version},
		{"CFBundleVersion", version},
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	for _, kv := range keys {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t<string>%s</string>\n", kv[0], xmlText(kv[1]))
	}
	b.WriteString("\t<key>LSUIElement</key>\n\t<true/>\n</dict>\n</plist>\n")
	return []byte(b.String())
}

// WriteBundle writes csquad.app: Info.plist, the icon and a copy of binary.
func WriteBundle(app, binary, version string) error {
	contents := filepath.Join(app, "Contents")
	if err := writeFile(filepath.Join(contents, "Info.plist"), InfoPlist(version)); err != nil {
		return err
	}
	if err := writeFile(filepath.Join(contents, "Resources", "csquad.icns"), ICNS()); err != nil {
		return err
	}
	return copyExe(binary, BundleExe(app))
}

func copyExe(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", dest, os.Getpid())
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Bundle posts through csquad.app: it runs the bundle's csquad (notify
// post), so the notification belongs to the bundle.
type Bundle struct {
	App string
	Env proc.Env
	// Wait is how long post waits for the user's answer to the permission
	// request; 0 = 15 s.
	Wait time.Duration
}

func (b Bundle) Name() string { return "macOS notification (" + b.App + ")" }

func (b Bundle) Send(ctx context.Context, m Message) error {
	wait := b.Wait
	if wait == 0 {
		wait = 15 * time.Second
	}
	cmd := exec.CommandContext(ctx, BundleExe(b.App), "notify", "post", "--title", m.Title, "--body", m.Body, "--wait", fmt.Sprint(wait.Milliseconds()))
	cmd.Env = b.Env
	return run(ctx, cmd, "csquad.app")
}

// Osascript is the fallback without csquad.app: AppleScript's display
// notification, which shows as Script Editor.
type Osascript struct{ Env proc.Env }

func (Osascript) Name() string {
	return "macOS notification through osascript (shows as Script Editor; csquad install sets up csquad.app)"
}

func (o Osascript) Send(ctx context.Context, m Message) error {
	// The texts go in as arguments: no AppleScript quoting.
	cmd := exec.CommandContext(ctx, "osascript", "-e", "on run argv", "-e", "display notification (item 2 of argv) with title (item 1 of argv)", "-e", "end run", m.Title, m.Body)
	cmd.Env = o.Env
	return run(ctx, cmd, "osascript")
}

func run(ctx context.Context, cmd *exec.Cmd, what string) error {
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	err := cmd.Run()
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return fmt.Errorf("%s did not finish in time", what)
	}
	if text := strings.TrimSpace(out.String()); text != "" {
		return fmt.Errorf("%s: %s", what, firstLines(text))
	}
	return fmt.Errorf("%s: %v", what, err)
}

// PlistString reads a string key of an Info.plist (for tests and checks).
func PlistString(plist []byte, key string) (string, bool) {
	d := xml.NewDecoder(bytes.NewReader(plist))
	var last string
	for {
		tok, err := d.Token()
		if err != nil {
			return "", false
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		var text string
		if se.Name.Local == "key" || se.Name.Local == "string" {
			if d.DecodeElement(&text, &se) != nil {
				return "", false
			}
		}
		switch se.Name.Local {
		case "key":
			last = text
		case "string":
			if last == key {
				return text, true
			}
		case "true":
			if last == key {
				return "true", true
			}
		}
	}
}
