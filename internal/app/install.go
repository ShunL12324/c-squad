package app

import (
	"fmt"

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

// Install installs or updates csquad.
func (a *App) Install(update bool) error {
	return install.Install(a.installOptions(), update)
}

// Uninstall removes csquad.
func (a *App) Uninstall() error {
	return install.Uninstall(a.installOptions())
}

// InstallStatus shows what is installed.
func (a *App) InstallStatus() {
	install.Status(a.installOptions())
}
