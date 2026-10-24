package install

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/ShunL12324/c-squad/internal/proc"
)

// npm installs: the `csquad` package's bin shim runs the platform package's
// binary with CSQUAD_NPM set to the package version, install records it in
// the manifest, and the binary copied to $CSQUAD_HOME/bin is what skills,
// hooks and the status line run, never the npx cache.

// NpmPackage is what `csquad update` fetches for an npm installation.
const NpmPackage = "csquad@latest"

// ViaNpm reports whether `update`, run as exe, has to hand over to npx: npm
// installed csquad and exe is that installed copy, which is the old version
// and has nothing newer to copy. A binary the shim started (CSQUAD_NPM set)
// or another build updates directly.
func ViaNpm(o Options, exe string) bool {
	if o.Source != "" || o.Env.Get("CSQUAD_NPM") != "" {
		return false
	}
	var m Manifest
	if !readJSON(o.manifestFile(), &m) || m.Npm == "" {
		return false
	}
	return sameFile(exe, BinaryPath(o.Home))
}

func sameFile(a, b string) bool {
	if r, err := filepath.EvalSymlinks(a); err == nil {
		a = r
	}
	if r, err := filepath.EvalSymlinks(b); err == nil {
		b = r
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// NpmUpdate runs `npx --yes csquad@latest update` with this process's stdio.
// The shim sets CSQUAD_NPM for the new binary, so it updates directly.
func NpmUpdate(ctx context.Context, o Options, stdin io.Reader, stdout, stderr io.Writer) error {
	args := []string{"--yes", NpmPackage, "update"}
	line := "npx " + strings.Join(args, " ")
	o.log("csquad %s was installed with npm; updating with: %s", o.Version, line)
	c, err := proc.Resolve("npx", args, o.Env)
	if err == nil {
		cmd := c.Command(ctx)
		cmd.Env = o.Env
		cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
		err = cmd.Run()
	}
	if err != nil {
		return fmt.Errorf("%s: %v (it needs Node.js and npm on PATH)", line, err)
	}
	return nil
}
