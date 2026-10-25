package notify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Installed is what install set up, kept in its manifest.
type Installed struct {
	Backend string        `json:"backend"`         // what install printed
	Files   []string      `json:"files,omitempty"` // in $CSQUAD_HOME: the icon, csquad.app
	AppID   *Registration `json:"appId,omitempty"` // the Windows app ID
	WSL     bool          `json:"wsl,omitempty"`   // the app ID was registered through powershell.exe
}

// Setup sets notifications up at install and removes them at uninstall.
type Setup struct {
	Sys     System
	Version string
	Binary  string // the installed csquad, copied into csquad.app
	// Registrar registers the Windows app ID; nil = the registry on
	// Windows, powershell.exe from WSL.
	Registrar Registrar
	// Exec runs codesign and lsregister on macOS; nil = for real.
	Exec func(name string, args ...string) error
}

// AppIDRegistrar is what registers the app ID: Registrar, else the
// registry on Windows, powershell.exe from WSL; nil without either.
func (s Setup) AppIDRegistrar(wsl bool) Registrar {
	if s.Registrar != nil {
		return s.Registrar
	}
	if wsl {
		env := s.Sys.Env
		return PowerShellAppID{Key: AppIDKey, Run: func(script string) (string, error) {
			ps := WSLPowerShell(env)
			if ps == "" {
				return "", errors.New("powershell.exe was not found")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			return RunPowerShell(ctx, ps, env, script)
		}}
	}
	if reg := SystemRegistry(); reg != nil {
		icon := AppIDIcon(s.Sys.Env)
		if icon == "" {
			icon = IconFile(s.Sys.Home)
		}
		return RegistryAppID{Reg: reg, Key: AppIDKey, Icon: icon}
	}
	return nil
}

func (s Setup) exec(name string, args ...string) error {
	if s.Exec != nil {
		return s.Exec(name, args...)
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", filepath.Base(name), firstLines(strings.TrimSpace(string(out)+" "+err.Error())))
	}
	return nil
}

// Install writes the icon (and on macOS csquad.app), registers the app ID
// on Windows and from WSL, and says which backend shows notifications. With
// CSQUAD_NOTIFY_FILE it sets nothing up. An error leaves the rest of the
// installation as it is; m still records what was made.
func (s Setup) Install() (m Installed, err error) {
	defer func() {
		if b, e := Select(s.Sys); e != nil {
			m.Backend = "none (" + e.Error() + ")"
		} else {
			m.Backend = b.Name()
		}
	}()
	if s.Sys.Env.Get("CSQUAD_NOTIFY_FILE") != "" {
		return m, nil
	}
	switch {
	case s.Sys.OS == "windows", s.Sys.WSL:
		wsl := s.Sys.OS != "windows"
		r := s.AppIDRegistrar(wsl)
		if r == nil {
			return m, errors.New("no registry to register the app ID in")
		}
		reg, err := r.Register(PNG(256))
		if err != nil {
			return m, fmt.Errorf("registering the app ID: %v", err)
		}
		m.AppID, m.WSL = &reg, wsl
	case s.Sys.OS == "darwin":
		app := BundleDir(s.Sys.Home)
		m.Files = append(m.Files, app)
		if err := WriteBundle(app, s.Binary, s.Version); err != nil {
			return m, err
		}
		if err := s.exec("codesign", "--force", "--sign", "-", app); err != nil {
			return m, err
		}
		if err := s.exec(Lsregister, "-f", app); err != nil {
			return m, err
		}
	default:
		icon := IconFile(s.Sys.Home)
		m.Files = append(m.Files, icon)
		if err := writeFile(icon, PNG(256)); err != nil {
			return m, err
		}
	}
	return m, nil
}

// RemoveStale removes the files an older install made that this one did not
// (csquad.png in $CSQUAD_HOME before the Windows icon moved to
// %LOCALAPPDATA%).
func (s Setup) RemoveStale(old, cur Installed) {
	for _, f := range old.Files {
		if !slices.Contains(cur.Files, f) {
			s.remove(f)
		}
	}
}

// remove removes a file or directory inside $CSQUAD_HOME, never outside.
func (s Setup) remove(f string) error {
	rel, err := filepath.Rel(s.Sys.Home, f)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil
	}
	if strings.HasSuffix(f, ".app") && s.Sys.OS == "darwin" {
		_ = s.exec(Lsregister, "-u", f)
	}
	return os.RemoveAll(f)
}

// Uninstall removes what Install made and returns what to tell the user.
// The app ID goes only while it still names this installation's icon: a
// Windows and a WSL installation can share it.
func (s Setup) Uninstall(m Installed) []string {
	var notes []string
	if m.AppID != nil {
		if r := s.AppIDRegistrar(m.WSL); r == nil {
			notes = append(notes, "Notifications: no registry; the app ID "+AppID+" is left")
		} else if removed, err := r.Unregister(*m.AppID); err != nil {
			notes = append(notes, fmt.Sprintf("Notifications: could not remove the app ID %s: %v", AppID, err))
		} else if !removed {
			notes = append(notes, "Notifications: the app ID "+AppID+" names another installation's icon; it is left")
		}
	}
	for _, f := range m.Files {
		if err := s.remove(f); err != nil {
			notes = append(notes, fmt.Sprintf("Notifications: could not remove %s: %v", f, err))
		}
	}
	return notes
}
