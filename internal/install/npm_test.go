package install

import (
	"path/filepath"
	"testing"
)

func TestInstallRecordsNpm(t *testing.T) {
	e := setup(t)
	e.install()
	var m Manifest
	readJSON(e.o.manifestFile(), &m)
	if m.Npm != "" {
		t.Fatalf("a source install recorded npm %q", m.Npm)
	}
	e.o.Env = e.o.Env.With("CSQUAD_NPM", "3.0.0")
	e.update()
	readJSON(e.o.manifestFile(), &m)
	if m.Npm != "3.0.0" {
		t.Fatalf("npm = %q, want 3.0.0", m.Npm)
	}
}

func TestUpdateGoesThroughNpxOnlyFromTheInstalledNpmCopy(t *testing.T) {
	e := setup(t)
	installed := BinaryPath(e.o.Home)
	other := e.o.Source
	plain := e.o
	plain.Source = "" // `csquad update` as users run it

	e.install()
	if ViaNpm(plain, installed) {
		t.Fatal("a source install went through npx")
	}

	e.o.Env = e.o.Env.With("CSQUAD_NPM", "3.0.0")
	e.install()
	if !ViaNpm(plain, installed) {
		t.Fatal("the installed copy of an npm install did not go through npx")
	}
	if ViaNpm(plain, other) {
		t.Fatal("another build went through npx instead of updating directly")
	}
	if ViaNpm(e.o, installed) {
		t.Fatal("the binary the npm shim started went through npx again")
	}
	if ViaNpm(plain, filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("a missing executable went through npx")
	}
}
