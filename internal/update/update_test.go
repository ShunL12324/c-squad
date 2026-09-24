package update

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ShunL12324/c-squad/internal/buildinfo"
)

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()
	must(t, os.MkdirAll(filepath.Dir(path), 0700))
	must(t, os.WriteFile(path, []byte(body), mode))
}

// fakes puts only the given fake tools on PATH, so no real package manager,
// dpkg or sudo is ever reached.
func fakes(t *testing.T, tools map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range tools {
		write(t, filepath.Join(dir, name), "#!/bin/sh\n"+body+"\n", 0700)
	}
	t.Setenv("PATH", dir)
	t.Setenv("CSQUAD_MEMBER_ID", "")
	t.Setenv("CSQUAD_STATE_DIR", "")
	return dir
}

// recordRuns replaces command execution with a recorder, so nothing installs.
func recordRuns(t *testing.T, tty, answer bool, effect func([]string) error) *[][]string {
	t.Helper()
	var ran [][]string
	previous := []any{runCommand, isTTY, confirm}
	runCommand = func(argv []string) error {
		ran = append(ran, argv)
		if effect != nil {
			return effect(argv)
		}
		return nil
	}
	isTTY = func() bool { return tty }
	confirm = func(string) bool { return answer }
	t.Cleanup(func() {
		runCommand, isTTY, confirm = previous[0].(func([]string) error), previous[1].(func() bool), previous[2].(func(string) bool)
	})
	return &ran
}

func runningFrom(t *testing.T, path string) {
	t.Helper()
	previous := executablePath
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = previous })
}

// npmInstall lays out a global npm prefix as npm itself does.
func npmInstall(t *testing.T, prefix, name string) string {
	t.Helper()
	write(t, filepath.Join(prefix, "lib", "node_modules", "csquad", "package.json"), `{"name":"`+name+`"}`, 0600)
	self := filepath.Join(prefix, "lib", "node_modules", "csquad", "native", "linux-amd64", "csquad")
	write(t, self, "#!/bin/sh\necho 'csquad 0.12.0'\n", 0700)
	return self
}

// npm is updated through the exact prefix that owns the binary, with that
// prefix's npm when it has one (nvm), otherwise the npm on PATH.
func TestNpmChannel(t *testing.T) {
	path := fakes(t, map[string]string{"npm": ""})
	prefix := filepath.Join(t.TempDir(), ".nvm", "versions", "node", "v24.13.0")
	self := npmInstall(t, prefix, "csquad")
	ch, err := Detect(self)
	must(t, err)
	want := [][]string{{filepath.Join(path, "npm"), "install", "--global", "--prefix", prefix, "csquad@latest"}}
	if ch.Name != "npm" || ch.Root || ch.Hint != "" || !reflect.DeepEqual(ch.Commands, want) || ch.Entry != self {
		t.Fatalf("npm on PATH: %+v", ch)
	}
	write(t, filepath.Join(prefix, "bin", "npm"), "#!/bin/sh\n", 0700)
	ch, err = Detect(self)
	must(t, err)
	if ch.Commands[0][0] != filepath.Join(prefix, "bin", "npm") {
		t.Fatalf("prefix npm not preferred: %v", ch.Commands)
	}
	if os.Getuid() != 0 {
		modules := filepath.Join(prefix, "lib", "node_modules")
		must(t, os.Chmod(modules, 0500))
		t.Cleanup(func() { _ = os.Chmod(modules, 0700) })
		ch, err = Detect(self)
		must(t, err)
		if !strings.Contains(ch.Hint, "sudo "+filepath.Join(prefix, "bin", "npm")+" install --global --prefix") {
			t.Fatalf("a root-owned prefix must be print-only: %+v", ch)
		}
	}
	other := npmInstall(t, filepath.Join(t.TempDir(), "usr"), "not-csquad")
	if ch, _ = Detect(other); ch.Name == "npm" {
		t.Fatal("a package not named csquad was taken for csquad")
	}
	for path, want := range map[string]string{
		"/home/u/.local/share/pnpm/global/5/node_modules/csquad/native/linux-amd64/csquad": "pnpm add --global",
		"/home/u/.bun/install/global/node_modules/csquad/native/linux-amd64/csquad":        "bun add --global",
	} {
		if ch, _ = Detect(path); ch.Hint == "" || !strings.Contains(ch.Hint, want) {
			t.Fatalf("%s: %+v", path, ch)
		}
	}
}

// brewInstall lays out a Homebrew prefix with a fake brew that knows it.
func brewInstall(t *testing.T, prefix, receiptTap string) string {
	t.Helper()
	cellar := filepath.Join(prefix, "Cellar")
	self := filepath.Join(cellar, "csquad", "0.11.1", "bin", "csquad")
	write(t, self, "#!/bin/sh\n", 0700)
	if receiptTap != "" {
		write(t, filepath.Join(cellar, "csquad", "0.11.1", "INSTALL_RECEIPT.json"), `{"source":{"tap":"`+receiptTap+`"}}`, 0600)
	}
	write(t, filepath.Join(prefix, "bin", "brew"), "#!/bin/sh\ncase \"$1\" in --cellar) echo "+cellar+";; list) echo 'git ShunL12324/c-squad/csquad tmux';; esac\n", 0700)
	return self
}

// Any Homebrew prefix is accepted when its own brew reports the Cellar; the
// formula comes from the receipt's tap, case-insensitively, or brew's list.
func TestHomebrewChannel(t *testing.T) {
	fakes(t, nil)
	prefix := filepath.Join(t.TempDir(), "some", "brew prefix")
	self := brewInstall(t, prefix, "ShunL12324/c-squad")
	ch, err := Detect(self)
	must(t, err)
	want := [][]string{{filepath.Join(prefix, "bin", "brew"), "upgrade", "shunl12324/c-squad/csquad"}}
	if ch.Name != "homebrew" || ch.Hint != "" || !reflect.DeepEqual(ch.Commands, want) || ch.Entry != filepath.Join(prefix, "opt", "csquad", "bin", "csquad") {
		t.Fatalf("homebrew: %+v", ch)
	}
	fallback := filepath.Join(t.TempDir(), "p")
	self = brewInstall(t, fallback, "")
	if ch, _ = Detect(self); ch.Commands[0][2] != "ShunL12324/c-squad/csquad" {
		t.Fatalf("list fallback: %+v", ch)
	}
	core := filepath.Join(t.TempDir(), "c")
	if ch, _ = Detect(brewInstall(t, core, "homebrew/core")); ch.Commands[0][2] != "csquad" {
		t.Fatalf("core formula: %+v", ch)
	}
	// A brew that reports another Cellar does not own this file.
	liar := filepath.Join(t.TempDir(), "liar")
	self = brewInstall(t, liar, "x/y")
	write(t, filepath.Join(liar, "bin", "brew"), "#!/bin/sh\necho /elsewhere/Cellar\n", 0700)
	if ch, _ = Detect(self); ch.Name == "homebrew" {
		t.Fatal("a brew for another Cellar was trusted")
	}
	if os.Getuid() != 0 {
		rack := filepath.Join(prefix, "Cellar", "csquad")
		must(t, os.Chmod(rack, 0500))
		t.Cleanup(func() { _ = os.Chmod(rack, 0700) })
		if ch, _ = Detect(filepath.Join(rack, "0.11.1", "bin", "csquad")); !strings.Contains(ch.Hint, "refuses to run as root") {
			t.Fatalf("unwritable Cellar: %+v", ch)
		}
	}
}

// A dpkg-owned binary with the csquad APT origin upgrades through apt-get
// under sudo; without the origin it is a local .deb.
func TestDpkgChannels(t *testing.T) {
	fakes(t, map[string]string{
		"dpkg-query": `[ "$1" = -S ] && echo "csquad: $2"`,
		"apt-cache":  `echo " 500 https://shunl12324.github.io/c-squad/apt stable/main amd64 Packages"`,
	})
	ch, err := Detect("/usr/bin/csquad")
	must(t, err)
	want := [][]string{{"sudo", "apt-get", "update"}, {"sudo", "apt-get", "install", "--only-upgrade", "csquad"}}
	if ch.Name != "apt" || !ch.Root || !reflect.DeepEqual(ch.Commands, want) {
		t.Fatalf("apt: %+v", ch)
	}
	write(t, filepath.Join(os.Getenv("PATH"), "apt-cache"), "#!/bin/sh\necho ' 100 /var/lib/dpkg/status'\n", 0700)
	if ch, _ = Detect("/usr/bin/csquad"); ch.Name != "deb" || !ch.Root || !ch.deb {
		t.Fatalf("local deb: %+v", ch)
	}
	write(t, filepath.Join(os.Getenv("PATH"), "dpkg-query"), "#!/bin/sh\necho 'dpkg-query: no path found' >&2; exit 1\n", 0700)
	if ch, _ = Detect("/home/u/bin/csquad"); ch.Name != "manual" || ch.Hint == "" {
		t.Fatalf("manual: %+v", ch)
	}
}

// release serves a fake GitHub release over TLS.
type release struct {
	server             *httptest.Server
	tag, deb, checksum string
	debBody            []byte
	status             int
	delay              time.Duration
}

func newRelease(t *testing.T, tag string) *release {
	t.Helper()
	r := &release{tag: tag, debBody: []byte("fake deb " + tag), status: http.StatusOK}
	r.deb = "csquad_" + strings.TrimPrefix(tag, "v") + "_amd64.deb"
	sum := sha256.Sum256(r.debBody)
	r.checksum = hex.EncodeToString(sum[:])
	r.server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(r.delay)
		switch req.URL.Path {
		case "/api":
			if r.status != http.StatusOK {
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.WriteHeader(r.status)
				return
			}
			_ = json.NewEncoder(w).Encode(Release{Tag: r.tag, Assets: []Asset{
				{Name: r.deb, URL: r.server.URL + "/deb"},
				{Name: "checksums.txt", URL: r.server.URL + "/sums"},
			}})
		case "/deb":
			_, _ = w.Write(r.debBody)
		case "/sums":
			fmt.Fprintf(w, "%s  %s\n%s  other.tar.gz\n", r.checksum, r.deb, strings.Repeat("0", 64))
		}
	}))
	t.Cleanup(r.server.Close)
	previous := []any{releaseAPI, httpClient, apiTimeout}
	client := r.server.Client()
	client.CheckRedirect = httpClient.CheckRedirect
	releaseAPI, httpClient, apiTimeout = r.server.URL+"/api", client, 2*time.Second
	t.Cleanup(func() {
		releaseAPI, httpClient, apiTimeout = previous[0].(string), previous[1].(*http.Client), previous[2].(time.Duration)
	})
	return r
}

func debTools(t *testing.T, installed, arch, fields string) {
	t.Helper()
	fakes(t, map[string]string{
		"dpkg":       "echo " + arch,
		"dpkg-query": `case "$1" in -S) echo "csquad: $2";; -W) printf '` + installed + `';; esac`,
		"apt-cache":  "echo ' 100 /var/lib/dpkg/status'",
		"dpkg-deb":   "printf '" + fields + "'",
	})
	t.Setenv("TMPDIR", t.TempDir())
}

func tmpLeftovers(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(os.Getenv("TMPDIR"))
	must(t, err)
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

// A local .deb is updated from the latest release only after the checksum and
// its own package fields match; every failure removes the download and never
// reaches sudo.
func TestLocalDebDownloadIsVerified(t *testing.T) {
	good := `Package: csquad\nVersion: 0.12.0\nArchitecture: amd64\n`
	for name, setup := range map[string]func(r *release) (installed, arch, fields, want string){
		"verified": func(*release) (string, string, string, string) { return "0.11.1", "amd64", good, "" },
		"checksum": func(r *release) (string, string, string, string) {
			r.debBody = []byte("tampered")
			return "0.11.1", "amd64", good, "checksums list"
		},
		"fields": func(*release) (string, string, string, string) {
			return "0.11.1", "amd64", `Package: csquad\nVersion: 0.12.1\nArchitecture: amd64\n`, "declares package"
		},
		"oversize": func(r *release) (string, string, string, string) {
			r.debBody = bytes.Repeat([]byte("x"), 4096)
			return "0.11.1", "amd64", good, "limit"
		},
		"not newer":    func(*release) (string, string, string, string) { return "0.12.0", "amd64", good, "nothing to update" },
		"architecture": func(*release) (string, string, string, string) { return "0.11.1", "i386", good, `for "i386"` },
		"rate limit": func(r *release) (string, string, string, string) {
			r.status = http.StatusForbidden
			return "0.11.1", "amd64", good, "rate limit"
		},
		"network timeout": func(r *release) (string, string, string, string) {
			r.delay = 3 * time.Second
			return "0.11.1", "amd64", good, "unable to query"
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newRelease(t, "v0.12.0")
			previous := maxDeb
			maxDeb = 1024
			t.Cleanup(func() { maxDeb = previous })
			installed, arch, fields, want := setup(r)
			debTools(t, installed, arch, fields)
			var out bytes.Buffer
			d, err := prepareDeb(&out)
			if want == "" {
				must(t, err)
				defer d.remove()
				data, err := os.ReadFile(d.path)
				must(t, err)
				if !bytes.Equal(data, r.debBody) || filepath.Base(d.path) != r.deb || !strings.Contains(out.String(), "not separately signed") {
					t.Fatalf("download %s: %q", d.path, out.String())
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("got %v, want %q", err, want)
			}
			if left := tmpLeftovers(t); len(left) != 0 {
				t.Fatalf("download left behind: %v", left)
			}
		})
	}
}

// Run: agents are refused, --check installs nothing, sudo needs a terminal,
// consent is required unless --yes, failures name the command, and the result
// is read from the channel's stable entry after the old path is gone.
func TestRunRules(t *testing.T) {
	prefix := filepath.Join(t.TempDir(), "prefix")
	setup := func(t *testing.T) (string, string) {
		fakes(t, map[string]string{"npm": ""})
		self := npmInstall(t, prefix, "csquad")
		runningFrom(t, self)
		return self, prefix
	}
	t.Run("member session", func(t *testing.T) {
		setup(t)
		ran := recordRuns(t, true, true, nil)
		t.Setenv("CSQUAD_MEMBER_ID", "worker")
		if err := Run(Options{Yes: true, Out: &bytes.Buffer{}}); !errors.Is(err, ErrMemberSession) || len(*ran) != 0 {
			t.Fatalf("agent update: %v %v", err, *ran)
		}
	})
	t.Run("check", func(t *testing.T) {
		setup(t)
		r := newRelease(t, "v0.12.0")
		ran := recordRuns(t, true, true, nil)
		var out bytes.Buffer
		must(t, Run(Options{Check: true, Out: &out}))
		if len(*ran) != 0 || !strings.Contains(out.String(), "Latest: v0.12.0") || !strings.Contains(out.String(), "npm install --global") {
			t.Fatalf("check: %v %s", *ran, out.String())
		}
		r.status = http.StatusForbidden
		out.Reset()
		must(t, Run(Options{Check: true, Out: &out}))
		if !strings.Contains(out.String(), "unable to query the latest release") || len(*ran) != 0 {
			t.Fatalf("check offline: %s", out.String())
		}
	})
	t.Run("consent", func(t *testing.T) {
		setup(t)
		ran := recordRuns(t, false, true, nil)
		if err := Run(Options{Out: &bytes.Buffer{}}); err == nil || len(*ran) != 0 {
			t.Fatalf("non-terminal without --yes ran: %v", *ran)
		}
		ran = recordRuns(t, true, false, nil)
		if err := Run(Options{Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "cancelled") || len(*ran) != 0 {
			t.Fatalf("declined: %v %v", err, *ran)
		}
	})
	t.Run("sudo needs a terminal", func(t *testing.T) {
		fakes(t, map[string]string{
			"dpkg-query": `echo "csquad: $2"`,
			"apt-cache":  `echo "https://shunl12324.github.io/c-squad/apt"`,
		})
		runningFrom(t, "/usr/bin/csquad")
		ran := recordRuns(t, false, true, nil)
		if err := Run(Options{Yes: true, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "sudo") || len(*ran) != 0 {
			t.Fatalf("sudo without a terminal: %v %v", err, *ran)
		}
	})
	t.Run("failure", func(t *testing.T) {
		setup(t)
		recordRuns(t, false, true, func([]string) error { return errors.New("exit status 1") })
		if err := Run(Options{Yes: true, Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "install --global --prefix") {
			t.Fatalf("failure: %v", err)
		}
	})
	t.Run("homebrew entry after the old keg is removed", func(t *testing.T) {
		fakes(t, nil)
		brew := filepath.Join(t.TempDir(), "brew")
		self := brewInstall(t, brew, "shunl12324/c-squad")
		runningFrom(t, self)
		entry := filepath.Join(brew, "opt", "csquad", "bin", "csquad")
		ran := recordRuns(t, true, true, func([]string) error {
			must(t, os.RemoveAll(filepath.Join(brew, "Cellar", "csquad", "0.11.1")))
			write(t, entry, "#!/bin/sh\necho 'csquad 0.12.0 (commit x, built y)'\n", 0700)
			return nil
		})
		var out bytes.Buffer
		teams := func() []Team {
			return []Team{{Name: "live", Active: true, Pinned: true, Version: buildinfo.Version}, {Name: "old", Active: true}}
		}
		must(t, Run(Options{Out: &out, Teams: teams}))
		text := out.String()
		for _, want := range []string{"Updated: csquad 0.12.0", "team live: csquad " + buildinfo.Version + " (pinned); resume it", "Warning: team old is running and not pinned", "team old: not pinned"} {
			if !strings.Contains(text, want) {
				t.Fatalf("report lacks %q:\n%s", want, text)
			}
		}
		if len(*ran) != 1 || (*ran)[0][1] != "upgrade" {
			t.Fatalf("ran %v", *ran)
		}
	})
}
