// Package pin keeps private, content-addressed copies of csquad builds so that a
// team keeps running the build it started with while package managers replace
// their own files. A pinned copy is identified by its path alone:
// <store>/<version>-<sha256>/csquad. See docs/design/update.md.
package pin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
)

// entryName matches a store entry directory: a version, then the full hash.
var entryName = regexp.MustCompile(`^(.+)-([0-9a-f]{64})$`)

// Pin is a parsed pinned path.
type Pin struct {
	Path    string
	Version string
	SHA256  string
}

// Parse reports whether path names a pinned copy and returns its version and
// hash. Anything else, including every path recorded before pinning existed,
// is unpinned.
func Parse(path string) (Pin, bool) {
	if !filepath.IsAbs(path) || filepath.Base(path) != "csquad" {
		return Pin{}, false
	}
	m := entryName.FindStringSubmatch(filepath.Base(filepath.Dir(path)))
	if m == nil {
		return Pin{}, false
	}
	return Pin{Path: path, Version: m[1], SHA256: m[2]}, true
}

// Dir returns the store directory: CSQUAD_VERSIONS_DIR, else
// ${XDG_DATA_HOME:-$HOME/.local/share}/csquad/versions.
func Dir() (string, error) {
	if d := os.Getenv("CSQUAD_VERSIONS_DIR"); d != "" {
		if !filepath.IsAbs(d) {
			return "", fmt.Errorf("CSQUAD_VERSIONS_DIR must be an absolute path: %q", d)
		}
		return filepath.Clean(d), nil
	}
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" || !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, "csquad", "versions"), nil
}

// secureDir creates dir (0700) if missing and checks that it is a real
// directory owned by this user and writable by nobody else.
func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	return privateOwned(dir, info, true)
}

func privateOwned(path string, info os.FileInfo, dir bool) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symlink; pinned csquad copies need a real path", path)
	}
	if dir && !info.IsDir() || !dir && !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular %s", path, map[bool]string{true: "directory", false: "file"}[dir])
	}
	if st, ok := info.Sys().(*syscall.Stat_t); ok && int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by uid %d, not by this user", path, st.Uid)
	}
	if info.Mode().Perm()&0022 != 0 {
		return fmt.Errorf("%s is writable by group or others (mode %v)", path, info.Mode().Perm())
	}
	return nil
}

var self struct {
	once sync.Once
	sha  string
	err  error
}

// SelfSHA256 hashes the running image once per process. On Linux it reads
// /proc/self/exe, the running inode even after the installed path was replaced
// or deleted; elsewhere it reads the resolved executable path.
func SelfSHA256() (string, error) {
	self.once.Do(func() {
		f, err := openSelf()
		if err != nil {
			self.err = err
			return
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		if _, err = io.Copy(h, f); err != nil {
			self.err = err
			return
		}
		self.sha = hex.EncodeToString(h.Sum(nil))
	})
	return self.sha, self.err
}

func openSelf() (*os.File, error) {
	if runtime.GOOS == "linux" {
		return os.Open("/proc/self/exe")
	}
	path, err := os.Executable()
	if err != nil {
		return nil, err
	}
	if path, err = filepath.EvalSymlinks(path); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return os.Open(path)
}

// Verify checks that a pinned copy is intact: a private regular file, not a
// symlink, whose content hashes to the value in its own path.
func Verify(p Pin) error {
	dirInfo, err := os.Lstat(filepath.Dir(p.Path))
	if err != nil {
		return err
	}
	if err = privateOwned(filepath.Dir(p.Path), dirInfo, true); err != nil {
		return err
	}
	info, err := os.Lstat(p.Path)
	if err != nil {
		return err
	}
	if err = privateOwned(p.Path, info, false); err != nil {
		return err
	}
	sha, err := fileSHA256(p.Path)
	if err != nil {
		return err
	}
	if sha != p.SHA256 {
		return fmt.Errorf("%s has content %s…, not the recorded %s…", p.Path, sha[:12], p.SHA256[:12])
	}
	return nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ErrReplaced reports that the installed file changed while this process ran,
// so the bytes on disk are not the running build.
var ErrReplaced = errors.New("csquad was replaced while running; run the command again")

// Create stores the running build and returns its pin. It is idempotent and
// safe to run concurrently: the entry name is content-addressed, and a corrupt
// existing entry is replaced by the verified copy.
func Create() (Pin, error) {
	root, err := Dir()
	if err != nil {
		return Pin{}, err
	}
	if err = secureDir(root); err != nil {
		return Pin{}, err
	}
	sha, err := SelfSHA256()
	if err != nil {
		return Pin{}, fmt.Errorf("hash the running csquad: %w", err)
	}
	p := Pin{Version: sanitizeVersion(buildinfo.Version), SHA256: sha}
	dir := filepath.Join(root, p.Version+"-"+sha)
	p.Path = filepath.Join(dir, "csquad")
	if err = secureDir(dir); err != nil {
		return Pin{}, err
	}
	if Verify(p) == nil {
		return p, nil
	}
	if err = writeCopy(dir, p); err != nil {
		return Pin{}, err
	}
	return p, Verify(p)
}

// sanitizeVersion keeps the version usable as a directory name segment.
func sanitizeVersion(v string) string {
	v = strings.Map(func(r rune) rune {
		if r == '/' || r == 0 || r == os.PathSeparator {
			return '_'
		}
		return r
	}, v)
	if v == "" {
		return "unknown"
	}
	return v
}

func writeCopy(dir string, p Pin) error {
	src, err := openSelf()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	tmp, err := os.OpenFile(filepath.Join(dir, ".csquad-"+strconv.Itoa(os.Getpid())+"-"+strconv.FormatInt(time.Now().UnixNano(), 36)), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0700)
	if err != nil {
		return err
	}
	name := tmp.Name()
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(name)
		}
	}()
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), src)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.SHA256 {
		return ErrReplaced
	}
	if err = probe(name); err != nil {
		return err
	}
	if err = os.Chmod(name, 0500); err != nil {
		return err
	}
	if err = os.Rename(name, p.Path); err != nil {
		return err
	}
	keep = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// probe runs a fresh copy before it is published. Tests replace it, since a
// test binary does not implement csquad version.
var probe = checkRuns

// checkRuns executes the copy and confirms it is this build. An exec failure
// usually means the store is on a noexec mount.
func checkRuns(path string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "version")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return fmt.Errorf("the copy of csquad in %s failed to run: %w", filepath.Dir(path), err)
		}
		return fmt.Errorf("cannot execute files in %s (noexec?); set CSQUAD_VERSIONS_DIR to an executable directory: %w", filepath.Dir(path), err)
	}
	if got := string(bytes.TrimSpace(out)); got != buildinfo.String() {
		return fmt.Errorf("%w (copy reports %q, this is %q)", ErrReplaced, got, buildinfo.String())
	}
	return nil
}

// Order is how a candidate build relates to a pinned one.
type Order int

// Order values.
const (
	Same Order = iota
	Newer
	Older
	Unknown
)

// Compare orders the running build against a pin. Only two stable X.Y.Z
// versions are ordered, numerically; everything else is Unknown.
func Compare(version, sha string, pinned Pin) Order {
	if sha == pinned.SHA256 {
		return Same
	}
	a, okA := stable(version)
	b, okB := stable(pinned.Version)
	if !okA || !okB || a == b {
		return Unknown
	}
	for i := range a {
		if a[i] != b[i] {
			if a[i] > b[i] {
				return Newer
			}
			return Older
		}
	}
	return Unknown
}

var stableVersion = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)$`)

func stable(v string) ([3]int, bool) {
	m := stableVersion.FindStringSubmatch(v)
	if m == nil {
		return [3]int{}, false
	}
	var out [3]int
	for i := range out {
		n, err := strconv.Atoi(m[i+1])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
