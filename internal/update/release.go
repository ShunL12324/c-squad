package update

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/internal/pin"
)

// releaseAPI is the GitHub endpoint for the latest stable release. GitHub
// excludes drafts and prereleases from it.
var releaseAPI = "https://api.github.com/repos/ShunL12324/c-squad/releases/latest"

// Download limits for the local .deb path; variables so tests can lower them.
var (
	maxDeb       int64 = 64 << 20
	maxChecksums int64 = 1 << 20
	maxAPI       int64 = 1 << 20
)

// httpClient follows redirects only to https and bounds every stage.
var httpClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 30 * time.Second}).DialContext,
		TLSHandshakeTimeout:   30 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" {
			return fmt.Errorf("refusing redirect to %s", req.URL.Redacted())
		}
		if len(via) >= 10 {
			return errors.New("too many redirects")
		}
		return nil
	},
}

// apiTimeout bounds the release query used by --check and the .deb path.
var apiTimeout = 10 * time.Second

// Release is the part of GitHub's release record update reads.
type Release struct {
	Tag    string  `json:"tag_name"`
	Assets []Asset `json:"assets"`
}

// Asset is one downloadable release file.
type Asset struct {
	Name string `json:"name"`
	URL  string `json:"browser_download_url"`
	Size int64  `json:"size"`
}

// Latest queries the latest stable release. Failures, including GitHub's
// unauthenticated rate limit, are returned for the caller to report.
func Latest() (Release, error) {
	client := *httpClient
	client.Timeout = apiTimeout
	req, err := http.NewRequest(http.MethodGet, releaseAPI, nil)
	if err != nil {
		return Release{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return Release{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		reason := resp.Status
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			reason += " (GitHub's rate limit for unauthenticated requests is exhausted; try again later)"
		}
		return Release{}, errors.New(reason)
	}
	var r Release
	if err = json.NewDecoder(io.LimitReader(resp.Body, maxAPI)).Decode(&r); err != nil {
		return Release{}, fmt.Errorf("unreadable release record: %w", err)
	}
	if _, ok := stableVersion(r.Tag); !ok {
		return Release{}, fmt.Errorf("latest release tag %q is not a stable vX.Y.Z", r.Tag)
	}
	return r, nil
}

func stableVersion(v string) ([3]int, bool) {
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	var out [3]int
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 || p != strconv.Itoa(n) {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func newer(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

type debPackage struct {
	dir, path string
}

func (d *debPackage) remove() { _ = os.RemoveAll(d.dir) }

// prepareDeb downloads and verifies the latest release .deb for this machine:
// the checksum from the same release and the package's own control fields.
// Nothing reaches sudo unless every check passed; the files are removed on
// every failure. Trust ends at GitHub's HTTPS and the release checksums file,
// which is not separately signed; the signed APT source is stronger.
func prepareDeb(w io.Writer) (*debPackage, error) {
	arch, err := output("dpkg", "--print-architecture")
	if err != nil {
		return nil, fmt.Errorf("dpkg --print-architecture: %w", err)
	}
	arch = strings.TrimSpace(arch)
	if arch != "amd64" && arch != "arm64" {
		return nil, fmt.Errorf("no csquad .deb is published for %q", arch)
	}
	installedText, err := output("dpkg-query", "-W", "-f=${Version}", "csquad")
	if err != nil {
		return nil, fmt.Errorf("read the installed csquad package version: %w", err)
	}
	installed, ok := stableVersion(strings.TrimSpace(installedText))
	if !ok {
		return nil, fmt.Errorf("installed csquad package version %q cannot be compared", strings.TrimSpace(installedText))
	}
	r, err := Latest()
	if err != nil {
		return nil, fmt.Errorf("unable to query the latest release: %w", err)
	}
	latest, _ := stableVersion(r.Tag)
	if !newer(latest, installed) {
		return nil, fmt.Errorf("csquad %s is installed; the latest release is %s, so there is nothing to update", strings.TrimSpace(installedText), r.Tag)
	}
	version := strings.TrimPrefix(r.Tag, "v")
	debName := "csquad_" + version + "_" + arch + ".deb"
	var deb, sums Asset
	for _, a := range r.Assets {
		switch a.Name {
		case debName:
			deb = a
		case "checksums.txt":
			sums = a
		}
	}
	if deb.URL == "" || sums.URL == "" {
		return nil, fmt.Errorf("release %s has no %s and checksums.txt", r.Tag, debName)
	}
	dir, err := os.MkdirTemp("", "csquad-update-")
	if err != nil {
		return nil, err
	}
	d := &debPackage{dir: dir, path: filepath.Join(dir, debName)}
	ok = false
	defer func() {
		if !ok {
			d.remove()
		}
	}()
	if err = os.Chmod(dir, 0700); err != nil {
		return nil, err
	}
	_, _ = fmt.Fprintf(w, "Downloading %s\n", deb.URL)
	if err = download(deb.URL, d.path, maxDeb); err != nil {
		return nil, err
	}
	sumsPath := filepath.Join(dir, "checksums.txt")
	if err = download(sums.URL, sumsPath, maxChecksums); err != nil {
		return nil, err
	}
	want, err := checksumFor(sumsPath, debName)
	if err != nil {
		return nil, err
	}
	got, err := fileSHA256(d.path)
	if err != nil {
		return nil, err
	}
	if got != want {
		return nil, fmt.Errorf("%s has SHA-256 %s, but the release checksums list %s", debName, got, want)
	}
	fields, err := output("dpkg-deb", "--field", d.path, "Package", "Version", "Architecture")
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", debName, err)
	}
	control := map[string]string{}
	for _, line := range strings.Split(fields, "\n") {
		if k, v, found := strings.Cut(line, ":"); found {
			control[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	if control["Package"] != "csquad" || control["Version"] != version || control["Architecture"] != arch {
		return nil, fmt.Errorf("%s declares package %q version %q architecture %q, not csquad %s %s", debName, control["Package"], control["Version"], control["Architecture"], version, arch)
	}
	_, _ = fmt.Fprintf(w, "Verified %s (SHA-256 %s) against the release checksums and its package fields.\n", debName, got)
	_, _ = fmt.Fprintln(w, "This checks GitHub's HTTPS download and the release checksums file, which is not separately signed; the signed APT source gives stronger verification (see docs/install.md).")
	ok = true
	return d, nil
}

func download(rawURL, dest string, limit int64) error {
	u, err := url.Parse(rawURL)
	if err != nil || u.Scheme != "https" {
		return fmt.Errorf("refusing to download %q: only https is allowed", rawURL)
	}
	resp, err := httpClient.Get(rawURL)
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(dest), err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", filepath.Base(dest), resp.Status)
	}
	if resp.ContentLength > limit {
		return fmt.Errorf("download %s: %d bytes exceeds the %d byte limit", filepath.Base(dest), resp.ContentLength, limit)
	}
	f, err := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, limit+1))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", filepath.Base(dest), err)
	}
	if n > limit {
		return fmt.Errorf("download %s: exceeds the %d byte limit", filepath.Base(dest), limit)
	}
	return nil
}

func checksumFor(path, name string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == name {
			if _, err := hex.DecodeString(fields[0]); err != nil || len(fields[0]) != 64 {
				return "", fmt.Errorf("checksums.txt has a malformed entry for %s", name)
			}
			return strings.ToLower(fields[0]), nil
		}
	}
	if err = scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("checksums.txt has no entry for %s", name)
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

// reportStore prints the pinned-copy store and its size. Copies are never
// removed automatically: a team in another project may still use one.
func reportStore(w io.Writer) {
	dir, err := pin.Dir()
	if err != nil {
		return
	}
	var size int64
	entries := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if info, e := d.Info(); e == nil {
				size += info.Size()
				entries++
			}
		}
		return nil
	})
	_, _ = fmt.Fprintf(w, "Pinned builds: %d in %s (%.1f MB); remove unused ones by hand\n", entries, dir, float64(size)/(1<<20))
}
