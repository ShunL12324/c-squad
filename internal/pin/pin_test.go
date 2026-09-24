package pin

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// useStore points the store at a fresh private directory and skips the probe:
// the test binary does not implement csquad version.
func useStore(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "versions")
	t.Setenv("CSQUAD_VERSIONS_DIR", dir)
	previous := probe
	probe = func(string) error { return nil }
	t.Cleanup(func() { probe = previous })
	return dir
}

func TestParse(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	for path, want := range map[string]bool{
		"/s/versions/0.12.0-" + sha + "/csquad":           true,
		"/s/v/dev-" + sha + "/csquad":                     true,
		"/s/v/0.12.0-rc.1-" + sha + "/csquad":             true,
		"/usr/bin/csquad":                                 false,
		"/s/v/0.12.0-" + sha[:63] + "/csquad":             false,
		"/s/v/0.12.0-" + strings.ToUpper(sha) + "/csquad": false,
		"relative/0.12.0-" + sha + "/csquad":              false,
		"/s/v/0.12.0-" + sha + "/other":                   false,
	} {
		p, ok := Parse(path)
		if ok != want {
			t.Fatalf("Parse(%q) = %v", path, ok)
		}
		if ok && p.SHA256 != sha {
			t.Fatalf("Parse(%q) hash %q", path, p.SHA256)
		}
	}
	if p, _ := Parse("/s/v/0.12.0-rc.1-" + sha + "/csquad"); p.Version != "0.12.0-rc.1" {
		t.Fatalf("version %q", p.Version)
	}
}

// Only two stable versions are ordered, numerically; everything else is
// Unknown, never decided by string order.
func TestCompare(t *testing.T) {
	a, b := strings.Repeat("a", 64), strings.Repeat("b", 64)
	pinned := func(v string) Pin { return Pin{Version: v, SHA256: b} }
	for _, test := range []struct {
		version, sha, pinned string
		want                 Order
	}{
		{"0.12.0", b, "0.9.0", Same},
		{"0.10.0", a, "0.9.0", Newer},
		{"v0.10.0", a, "0.9.9", Newer},
		{"0.9.0", a, "0.10.0", Older},
		{"1.0.0", a, "0.99.99", Newer},
		{"0.12.0", a, "0.12.0", Unknown},
		{"dev", a, "0.12.0", Unknown},
		{"0.12.0", a, "dev", Unknown},
		{"0.13.0-rc.1", a, "0.12.0", Unknown},
		{"unknown", a, "unknown", Unknown},
	} {
		if got := Compare(test.version, test.sha, pinned(test.pinned)); got != test.want {
			t.Fatalf("Compare(%s vs %s) = %v, want %v", test.version, test.pinned, got, test.want)
		}
	}
}

func selfBytes(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("/proc/self/exe")
	if err != nil {
		t.Skip("needs /proc/self/exe")
	}
	return data
}

// A pin is an independent private copy: not a link to the running file, mode
// 0500 in a 0700 directory, named by its full hash, and idempotent.
func TestCreateMakesAPrivateIndependentCopy(t *testing.T) {
	store := useStore(t)
	data := selfBytes(t)
	sum := sha256.Sum256(data)
	p, err := Create()
	must(t, err)
	if p.SHA256 != hex.EncodeToString(sum[:]) || filepath.Dir(filepath.Dir(p.Path)) != store {
		t.Fatalf("pin %+v", p)
	}
	info, err := os.Lstat(p.Path)
	must(t, err)
	if info.Mode().Perm() != 0500 || !info.Mode().IsRegular() {
		t.Fatalf("mode %v", info.Mode())
	}
	self, err := os.Stat("/proc/self/exe")
	must(t, err)
	if os.SameFile(info, self) || info.Sys().(*syscall.Stat_t).Nlink != 1 {
		t.Fatal("the pin shares an inode with the running file")
	}
	for _, dir := range []string{store, filepath.Dir(p.Path)} {
		d, err := os.Lstat(dir)
		must(t, err)
		if d.Mode().Perm() != 0700 {
			t.Fatalf("%s mode %v", dir, d.Mode().Perm())
		}
	}
	again, err := Create()
	must(t, err)
	if again != p {
		t.Fatalf("second pin %+v", again)
	}
	entries, err := os.ReadDir(filepath.Dir(p.Path))
	must(t, err)
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

// Concurrent pins of one build leave exactly one valid file.
func TestCreateIsSafeConcurrently(t *testing.T) {
	useStore(t)
	selfBytes(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := Create(); errs <- err }()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	p, err := Create()
	must(t, err)
	must(t, Verify(p))
	entries, _ := os.ReadDir(filepath.Dir(p.Path))
	if len(entries) != 1 {
		t.Fatalf("leftover files: %v", entries)
	}
}

// A damaged entry fails verification and is replaced by the next pin; a
// missing one likewise.
func TestVerifyDetectsDamageAndCreateRepairs(t *testing.T) {
	useStore(t)
	selfBytes(t)
	p, err := Create()
	must(t, err)
	must(t, os.Chmod(p.Path, 0700))
	must(t, os.WriteFile(p.Path, []byte("#!/bin/sh\n"), 0700))
	if err = Verify(p); err == nil || !strings.Contains(err.Error(), "not the recorded") {
		t.Fatalf("damaged copy verified: %v", err)
	}
	if _, err = Create(); err != nil {
		t.Fatal(err)
	}
	must(t, Verify(p))
	must(t, os.Remove(p.Path))
	if err = Verify(p); err == nil {
		t.Fatal("missing copy verified")
	}
	_, err = Create()
	must(t, err)
	must(t, Verify(p))
}

// The store and its entries must be private real directories.
func TestInsecureStoreIsRefused(t *testing.T) {
	store := useStore(t)
	selfBytes(t)
	must(t, os.MkdirAll(store, 0700))
	must(t, os.Chmod(store, 0777))
	if _, err := Create(); err == nil || !strings.Contains(err.Error(), "writable by group or others") {
		t.Fatalf("world-writable store accepted: %v", err)
	}
	must(t, os.Chmod(store, 0700))
	target := filepath.Join(t.TempDir(), "elsewhere")
	must(t, os.Mkdir(target, 0700))
	link := filepath.Join(t.TempDir(), "link")
	must(t, os.Symlink(target, link))
	t.Setenv("CSQUAD_VERSIONS_DIR", link)
	if _, err := Create(); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked store accepted: %v", err)
	}
	t.Setenv("CSQUAD_VERSIONS_DIR", "relative/dir")
	if _, err := Create(); err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative store accepted: %v", err)
	}
}

// A copy the system will not execute (a noexec mount) is reported as such,
// and a copy that is not this build is refused as replaced.
func TestProbeReportsNoexecAndReplacement(t *testing.T) {
	dir := t.TempDir()
	notExec := filepath.Join(dir, "csquad")
	must(t, os.WriteFile(notExec, []byte("#!/bin/sh\necho hi\n"), 0600))
	if err := checkRuns(notExec); err == nil || !strings.Contains(err.Error(), "noexec") || !strings.Contains(err.Error(), "CSQUAD_VERSIONS_DIR") {
		t.Fatalf("exec failure: %v", err)
	}
	other := filepath.Join(dir, "other")
	must(t, os.WriteFile(other, []byte("#!/bin/sh\necho csquad 9.9.9\n"), 0700))
	if err := checkRuns(other); !errors.Is(err, ErrReplaced) {
		t.Fatalf("different build accepted: %v", err)
	}
}
