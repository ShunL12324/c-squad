package app

import (
	"context"
	"fmt"
	"os"

	"github.com/ShunL12324/c-squad/internal/install"
)

func (a *App) installOptions() install.Options {
	return install.Options{
		ClaudeDir: a.ClaudeDir,
		Home:      a.Home,
		Version:   Version,
		Cwd:       a.Cwd,
		Env:       a.Env,
		Source:    a.Env.Get("CSQUAD_INSTALL_SOURCE"),
		Log:       func(s string) { fmt.Fprintln(a.Stdout, s) },
	}
}

// Install installs or updates csquad. The installed copy of an npm
// installation updates through npx: it is the old version itself.
func (a *App) Install(update bool) error {
	o := a.installOptions()
	if update {
		if exe, err := os.Executable(); err == nil && install.ViaNpm(o, exe) {
			return install.NpmUpdate(context.Background(), o, a.Stdin, a.Stdout, a.Stderr)
		}
	}
	return install.Install(o, update)
}

// Uninstall removes csquad.
func (a *App) Uninstall() error {
	return install.Uninstall(a.installOptions())
}

// InstallStatus shows what is installed.
func (a *App) InstallStatus() {
	install.Status(a.installOptions())
}
