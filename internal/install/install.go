// Package install installs, updates, removes and inspects csquad for Claude
// Code: the binary in $CSQUAD_HOME/bin, the skills in
// $CLAUDE_CONFIG_DIR/skills, and the status line wrapper in settings.json;
// and for Codex, when $CODEX_HOME exists: the skills in ~/.agents/skills.
package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ShunL12324/c-squad/assets"
	"github.com/ShunL12324/c-squad/internal/notify"
	"github.com/ShunL12324/c-squad/internal/proc"
)

// MinClaude is the oldest Claude Code finish works fully with: `claude rm
// --discard-unpushed` / `--force-remove-worktree` came in 2.1.260.
var MinClaude = [3]int{2, 1, 260}

// DefaultRefresh is the status line refresh interval set when the user's own
// status line has none (seconds).
const DefaultRefresh = 10

// Options locate an installation.
type Options struct {
	ClaudeDir string // $CLAUDE_CONFIG_DIR
	Home      string // $CSQUAD_HOME
	Version   string
	Source    string // the binary to install; "" = this executable
	Cwd       string // for the project-override warning
	CodexHome string // $CODEX_HOME; "" = from Env
	AgentsDir string // ~/.agents, where Codex reads skills; "" = from Env
	Env       proc.Env
	Log       func(string)
}

func (o Options) log(format string, args ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, args...))
	}
}

// BinaryPath is where the installed binary lives.
func BinaryPath(home string) string {
	name := "csquad"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(home, "bin", name)
}

// Manifest records what install wrote.
type Manifest struct {
	Version   string            `json:"version"`
	ClaudeDir string            `json:"claudeDir"`
	Binary    string            `json:"binary"`
	Files     map[string]string `json:"files"`        // claude-dir relative path → sha256
	Settings  string            `json:"settingsHash"` // sha256 of settings.json as csquad last wrote it
	// Command is the status line command install wrote: on Windows it may
	// name the binary below ~, which depends on the environment.
	Command string `json:"statusLineCommand,omitempty"`
	// Codex: what install wrote below AgentsDir, when $CODEX_HOME existed.
	AgentsDir  string            `json:"agentsDir,omitempty"`
	CodexFiles map[string]string `json:"codexFiles,omitempty"` // agents-dir relative path → sha256
	// SharedSkills: AgentsDir/skills was the Claude skills directory itself
	// (one linked to the other). Files then holds the one shared copy,
	// written with both hosts' instructions, and CodexFiles is empty.
	SharedSkills bool `json:"sharedSkills,omitempty"`
	// HooksText is the text install inserted into Codex's hooks.json (the
	// whole file when install created it); uninstall removes exactly that.
	HooksText string `json:"hooksText,omitempty"`
	// Npm is the version of the npm package that installed csquad; "" for a
	// build from source. See npm.go.
	Npm string `json:"npm,omitempty"`
	// Notify is what install set up for the notifications: the icon,
	// csquad.app (macOS), the app ID (Windows, WSL).
	Notify *notify.Installed `json:"notify,omitempty"`
}

// Saved is the user's own status line, kept in $CSQUAD_HOME/statusline.json.
type Saved struct {
	Present         bool            `json:"present"`
	StatusLine      json.RawMessage `json:"statusLine,omitempty"`
	CreatedSettings bool            `json:"createdSettings,omitempty"`
	Raw             string          `json:"raw,omitempty"` // the original value, byte for byte
}

// CommandLine is the user's status line command, "" when there is none.
func (s Saved) CommandLine() string {
	var sl struct {
		Command string `json:"command"`
	}
	if s.Present && json.Unmarshal(s.StatusLine, &sl) == nil {
		return sl.Command
	}
	return ""
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func readJSON(file string, v any) bool {
	b, err := os.ReadFile(file)
	return err == nil && json.Unmarshal(b, v) == nil
}

func writeJSON(file string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(file, append(b, '\n'), 0o644)
}

func (o Options) manifestFile() string { return filepath.Join(o.Home, "manifest.json") }
func (o Options) savedFile() string    { return filepath.Join(o.Home, "statusline.json") }
func (o Options) backupFile() string   { return filepath.Join(o.Home, "settings.json.bak") }
func (o Options) settingsFile() string { return filepath.Join(o.ClaudeDir, "settings.json") }

// LoadSaved reads the user's saved status line.
func LoadSaved(home string) (Saved, bool) {
	var s Saved
	ok := readJSON(filepath.Join(home, "statusline.json"), &s)
	return s, ok
}

// StatuslineCommand is the wrapper command written into settings.json;
// home is the directory ~ names (see assets.ProgramPath).
func StatuslineCommand(binary string, win bool, home string) string {
	return assets.ProgramPath(binary, win, home) + " statusline"
}

var (
	oursRe   = regexp.MustCompile(`csquad(\.exe)?["']? statusline$`)
	legacyRe = regexp.MustCompile(`csquad[\\/]tasks\.js["']? statusline$`)
)

// IsOurs reports whether a status line command is csquad's wrapper, whatever
// path it names.
func IsOurs(command string) bool { return oursRe.MatchString(command) }

// legacy refuses a csquad 1.x installation (its manifest, or a status line
// pointing at its tasks.js). 3.x does not migrate it: 1.x's own uninstall
// puts the user's status line back first, so it is never lost.
func (o Options) legacy(d *document) error {
	dir := filepath.Join(o.ClaudeDir, "csquad")
	_, err := os.Stat(filepath.Join(dir, "manifest.json"))
	found := err == nil
	var current struct {
		Command string `json:"command"`
	}
	if d != nil && d.value("statusLine", &current) && legacyRe.MatchString(current.Command) {
		found = true
	}
	if found {
		return fmt.Errorf("csquad 1.x is installed in %s; run `npx csquad@1 uninstall` first, nothing was changed", dir)
	}
	return nil
}

func loadDocument(file string) (*document, bool, error) {
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	d, err := parseDocument(raw)
	if err != nil {
		return nil, true, fmt.Errorf("%s is %v; fix it first, nothing was changed", file, err)
	}
	return d, true, nil
}

// checkPaths refuses, before anything is written, the layouts where a write
// would replace a link or fail half way: a dangling settings.json link whose
// target directory is missing, symlinked files csquad manages, dangling
// directory links.
func (o Options) checkPaths(rels []string) error {
	settings := o.settingsFile()
	target, err := resolveLink(settings)
	if err != nil {
		return err
	}
	if isLink(settings) {
		if _, err := os.Stat(filepath.Dir(target)); err != nil {
			return fmt.Errorf("%s is a symlink to %s, whose directory does not exist; create it first, nothing was changed", settings, target)
		}
	}
	dirs := map[string]bool{o.ClaudeDir: true, o.Home: true}
	files := []string{BinaryPath(o.Home), o.manifestFile(), o.savedFile(), o.backupFile()}
	for _, rel := range rels {
		files = append(files, filepath.Join(o.ClaudeDir, filepath.FromSlash(rel)))
		parts := strings.Split(rel, "/")
		for i := 1; i < len(parts); i++ {
			dirs[filepath.Join(o.ClaudeDir, filepath.Join(parts[:i]...))] = true
		}
	}
	dirs[filepath.Dir(BinaryPath(o.Home))] = true
	for _, f := range files {
		if isLink(f) {
			return fmt.Errorf("%s is a symlink; csquad manages this file itself, remove the link first, nothing was changed", f)
		}
	}
	for d := range dirs {
		if isDirLink(d) {
			if _, err := os.Stat(d); err != nil {
				return fmt.Errorf("%s is a dangling symlink; fix or remove it first, nothing was changed", d)
			}
		}
	}
	return nil
}

// skillFiles returns relative path → rendered content of every file of the
// skills for a host (claude or codex).
func (o Options) skillFiles(host string) map[string][]byte {
	binary := BinaryPath(o.Home)
	win := runtime.GOOS == "windows"
	triggers := Triggers(filepath.Join(o.Home, "models.md"))
	out := map[string][]byte{}
	for name, text := range assets.SkillFilesFor(host) {
		text = assets.Render(text, binary, win, o.Env.TildeHome())
		if name == "create-task/SKILL.md" && triggers != "" {
			text = extendDescription(text, triggers)
		}
		out["skills/"+name] = []byte(text)
	}
	return out
}

// Install installs csquad, or updates it (update requires an installation).
func Install(o Options, update bool) error {
	var old Manifest
	hasOld := readJSON(o.manifestFile(), &old)
	if update && !hasOld {
		return fmt.Errorf("csquad is not installed (no %s); run: csquad install", o.manifestFile())
	}
	// Parse settings first so invalid JSON aborts before anything is written.
	d, _, err := loadDocument(o.settingsFile())
	if err != nil {
		return err
	}
	if err := o.legacy(d); err != nil {
		return err
	}
	files := o.skillFiles("claude")
	if err := o.checkPaths(sorted(files)); err != nil {
		return err
	}
	codex, err := o.planCodex()
	if err != nil {
		return err
	}
	if codex.files != nil && o.sharedSkills(codex.dir) {
		// Both hosts read the same files: one copy with both hosts' blocks.
		files, codex.shared = o.skillFiles(assets.Shared), true
	}
	known := knownHashes(old)
	if err := os.MkdirAll(o.Home, 0o755); err != nil {
		return err
	}
	if err := o.installBinary(); err != nil {
		return err
	}
	m := Manifest{Version: o.Version, ClaudeDir: o.ClaudeDir, Binary: BinaryPath(o.Home), Settings: old.Settings, Npm: o.Env.Get("CSQUAD_NPM")}
	if m.Files, err = o.writeManaged(o.ClaudeDir, files, old.Files, known); err != nil {
		return err
	}
	if err := o.installCodex(codex, &m, old, known); err != nil {
		return err
	}
	hash, err := o.wrapStatusLine(m.Settings)
	if err != nil {
		return err
	}
	m.Settings = hash
	m.Command = StatuslineCommand(BinaryPath(o.Home), runtime.GOOS == "windows", o.Env.TildeHome())
	if err := o.installCodexHook(&m, old); err != nil {
		return err
	}
	// Notifications never stop an install: a failed step is a warning.
	nm, nerr := o.notifySetup().Install()
	m.Notify = &nm
	if err := writeJSON(o.manifestFile(), m); err != nil {
		return err
	}
	verb := "Installed"
	if update {
		verb = "Updated"
	}
	o.log("%s csquad %s: %s, skills in %s", verb, o.Version, BinaryPath(o.Home), filepath.Join(o.ClaudeDir, "skills"))
	if nerr != nil {
		o.log("Warning: notifications are not fully set up: %v", nerr)
	}
	o.log("Notifications: %s", nm.Backend)
	o.warnProjectOverride()
	o.warnClaudeVersion()
	return nil
}

func (o Options) notifySetup() notify.Setup {
	return notify.Setup{Sys: notify.Local(o.Env, o.Home), Version: o.Version, Binary: BinaryPath(o.Home)}
}

// installBinary copies the running (or given) binary to $CSQUAD_HOME/bin.
func (o Options) installBinary() error {
	src := o.Source
	if src == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		src = exe
	}
	if r, err := filepath.EvalSymlinks(src); err == nil {
		src = r
	}
	dest := BinaryPath(o.Home)
	if r, err := filepath.EvalSymlinks(dest); err == nil && r == src {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".new"
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
		return err
	}
	if runtime.GOOS == "windows" {
		// A running .exe cannot be replaced, but it can be renamed: only then
		// is it moved aside, so an install leaves no .old behind otherwise.
		os.Remove(dest + ".old")
		if os.Rename(tmp, dest) == nil {
			return nil
		}
		_ = os.Rename(dest, dest+".old")
	}
	if err := renameRetry(tmp, dest); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// wrapStatusLine points settings.json's statusLine at the wrapper, saving the
// user's own first. It returns the hash of settings.json as written.
func (o Options) wrapStatusLine(lastHash string) (string, error) {
	file := o.settingsFile()
	d, exists, err := loadDocument(file)
	if err != nil {
		return "", err
	}
	command := StatuslineCommand(BinaryPath(o.Home), runtime.GOOS == "windows", o.Env.TildeHome())
	if !exists {
		d, _ = parseDocument([]byte("{}"))
	}
	var current struct {
		Command         string   `json:"command"`
		RefreshInterval *float64 `json:"refreshInterval"`
	}
	hasLine := d.value("statusLine", &current)
	if hasLine && IsOurs(current.Command) {
		// Already wrapped: never wrap twice, and keep the saved original.
		if _, ok := LoadSaved(o.Home); !ok {
			if err := writeJSON(o.savedFile(), Saved{Present: false}); err != nil {
				return "", err
			}
		}
		if current.Command == command {
			return orHash(lastHash, d.raw), nil
		}
		raw := d.set("statusLine", d.format(statusLineValue(command, current.RefreshInterval)))
		if err := writeAtomic(file, raw, 0o644); err != nil {
			return "", err
		}
		return sha(raw), nil
	}
	saved := Saved{Present: false, CreatedSettings: !exists}
	if v, ok := d.rawValue("statusLine"); ok {
		saved = Saved{Present: true, StatusLine: json.RawMessage(v), Raw: v}
	}
	if err := writeJSON(o.savedFile(), saved); err != nil {
		return "", err
	}
	if exists {
		if _, err := os.Stat(o.backupFile()); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(o.backupFile(), d.raw, 0o600); err != nil {
				return "", err
			}
		}
	}
	raw := d.set("statusLine", d.format(statusLineValue(command, current.RefreshInterval)))
	if !exists {
		raw = append(raw, '\n')
	}
	if err := writeAtomic(file, raw, 0o644); err != nil {
		return "", err
	}
	if saved.Present {
		o.log("Status line: wrapped your own (saved to %s)", o.savedFile())
	} else {
		o.log("Status line: set")
	}
	return sha(raw), nil
}

func orHash(h string, raw []byte) string {
	if h != "" {
		return h
	}
	return sha(raw)
}

func statusLineValue(command string, refresh *float64) orderedObject {
	r := any(DefaultRefresh)
	if refresh != nil {
		r = *refresh
	}
	return orderedObject{{"type", "command"}, {"command", command}, {"refreshInterval", r}}
}

func sorted(files map[string][]byte) []string {
	rels := make([]string, 0, len(files))
	for rel := range files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	return rels
}

// sharedSkills reports whether the Claude skills directory and the one
// Codex reads below agentsDir are the same directory: one a symlink or
// junction to the other, or both to a third.
func (o Options) sharedSkills(agentsDir string) bool {
	return sameFile(filepath.Join(o.ClaudeDir, "skills"), filepath.Join(agentsDir, "skills"))
}

// knownHashes is, per relative path, every content csquad wrote there last
// time, for Claude Code and for Codex. A skills directory both share holds
// either one (before 3.0.2 they were written over each other), and neither
// is an edit of the user's.
func knownHashes(m Manifest) map[string][]string {
	known := map[string][]string{}
	for _, files := range []map[string]string{m.Files, m.CodexFiles} {
		for rel, hash := range files {
			known[rel] = append(known[rel], hash)
		}
	}
	return known
}

// writeManaged writes files (relative path → content) below root and
// returns their hashes. A file that holds none of the contents csquad wrote
// last time (known) is saved as .bak first; files of old that are no longer
// shipped go.
func (o Options) writeManaged(root string, files map[string][]byte, old map[string]string, known map[string][]string) (map[string]string, error) {
	hashes := map[string]string{}
	for _, rel := range sorted(files) {
		content := files[rel]
		dest := filepath.Join(root, filepath.FromSlash(rel))
		hash := sha(content)
		current, err := os.ReadFile(dest)
		exists := err == nil
		if exists && sha(current) != hash && !slices.Contains(known[rel], sha(current)) {
			// Not what csquad installed last time: keep the user's version.
			if err := os.WriteFile(dest+".bak", current, 0o644); err != nil {
				return hashes, err
			}
			o.log("%s was edited; saved your version as %s.bak", rel, rel)
		}
		if !exists || sha(current) != hash {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return hashes, err
			}
			if err := os.WriteFile(dest, content, 0o644); err != nil {
				return hashes, err
			}
		}
		hashes[rel] = hash
	}
	// Files an older version shipped that this one no longer does.
	for rel := range old {
		if _, ok := hashes[rel]; !ok {
			o.removeFrom(root, rel, known[rel])
		}
	}
	return hashes, nil
}

// removeManaged deletes a file csquad installed; an edited one is renamed to
// .bak.
func (o Options) removeManaged(rel string, hashes []string) { o.removeFrom(o.ClaudeDir, rel, hashes) }

// removeFrom deletes a file holding one of the contents csquad wrote
// (hashes); any other content is the user's edit, renamed to .bak.
func (o Options) removeFrom(root, rel string, hashes []string) {
	file := filepath.Join(root, filepath.FromSlash(rel))
	b, err := os.ReadFile(file)
	if err != nil {
		return
	}
	if !slices.Contains(hashes, sha(b)) {
		if err := renameRetry(file, file+".bak"); err == nil {
			o.log("%s was edited; kept your version as %s.bak", rel, rel)
		}
	} else {
		os.Remove(file)
	}
	// Drop the skill's directories once they are empty.
	for parent := filepath.Dir(file); parent != root && parent != filepath.Join(root, "skills") && strings.HasPrefix(parent, root); parent = filepath.Dir(parent) {
		rmdirIfEmpty(parent)
	}
}

// rmdirIfEmpty removes an empty directory. A symlink or junction (a skills/
// directory shared with other tools) is never csquad's to remove: on Windows
// removing it would delete the link itself.
func rmdirIfEmpty(d string) {
	if isDirLink(d) {
		return
	}
	if entries, err := os.ReadDir(d); err == nil && len(entries) == 0 {
		os.Remove(d)
	}
}

// Uninstall restores the status line and removes what csquad installed.
// The database and models.md stay: they are the user's.
func Uninstall(o Options) error {
	var m Manifest
	if !readJSON(o.manifestFile(), &m) {
		return fmt.Errorf("csquad is not installed (no %s)", o.manifestFile())
	}
	if err := o.restoreStatusLine(m); err != nil {
		return err
	}
	// Asked before any file goes: an emptied shared skills directory stays,
	// or the link to it would dangle.
	shared := o.sharedSkills(o.agentsDirOf(m))
	known := knownHashes(m)
	for rel := range m.Files {
		o.removeManaged(rel, known[rel])
	}
	o.uninstallCodexHook(m)
	if !shared {
		rmdirIfEmpty(filepath.Join(o.ClaudeDir, "skills"))
	}
	o.uninstallCodex(m, known)
	if m.Notify != nil {
		for _, note := range o.notifySetup().Uninstall(*m.Notify) {
			o.log("%s", note)
		}
	}
	for _, f := range []string{o.manifestFile(), o.savedFile(), o.backupFile()} {
		os.Remove(f)
	}
	for _, d := range []string{"settings", "prompts", "logs", "cache"} {
		os.RemoveAll(filepath.Join(o.Home, d))
	}
	bin := BinaryPath(o.Home)
	os.Remove(bin + ".old")
	if err := os.Remove(bin); err != nil && !errors.Is(err, os.ErrNotExist) {
		// Windows: the running binary cannot be deleted, only renamed.
		if os.Rename(bin, bin+".old") != nil {
			o.log("Could not remove %s (%v); delete it by hand", bin, err)
		}
	}
	rmdirIfEmpty(filepath.Dir(bin))
	rmdirIfEmpty(o.Home)
	if entries, err := os.ReadDir(o.Home); err == nil && len(entries) > 0 {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		o.log("Left %s in place; it still holds: %s", o.Home, strings.Join(names, ", "))
	}
	o.log("Uninstalled csquad. Running task sessions were not touched.")
	return nil
}

func (o Options) restoreStatusLine(m Manifest) error {
	file := o.settingsFile()
	d, exists, err := loadDocument(file)
	if err != nil {
		return err
	}
	saved, _ := LoadSaved(o.Home)
	var current struct {
		Command string `json:"command"`
	}
	command := m.Command
	if command == "" { // a manifest from before it was recorded
		command = StatuslineCommand(m.Binary, runtime.GOOS == "windows", o.Env.TildeHome())
	}
	if !exists || !d.value("statusLine", &current) || current.Command != command {
		if exists {
			o.log("Status line: settings.json no longer points at the csquad wrapper; left as it is")
		}
		return nil
	}
	defer o.log("Status line: restored")
	// Unchanged since csquad wrote it: put back the original bytes.
	if m.Settings != "" && sha(d.raw) == m.Settings {
		if saved.CreatedSettings {
			target, err := resolveLink(file)
			if err != nil {
				return err
			}
			return os.Remove(target)
		}
		if bak, err := os.ReadFile(o.backupFile()); err == nil {
			return writeAtomic(file, bak, 0o644)
		}
	}
	// Edited since: change only the statusLine member back.
	var raw []byte
	if saved.Present {
		original := saved.Raw
		if original == "" {
			original = string(saved.StatusLine)
		}
		raw = d.set("statusLine", original)
	} else {
		raw = d.remove("statusLine")
	}
	if saved.CreatedSettings {
		if nd, err := parseDocument(raw); err == nil && len(nd.members) == 0 {
			target, err := resolveLink(file)
			if err != nil {
				return err
			}
			return os.Remove(target)
		}
	}
	return writeAtomic(file, raw, 0o644)
}

// Status prints what is installed.
func Status(o Options) {
	o.log("Claude dir:  %s", o.ClaudeDir)
	o.log("csquad home: %s", o.Home)
	o.log("This binary: csquad %s", o.Version)
	var m Manifest
	if !readJSON(o.manifestFile(), &m) {
		o.log("Installed:   no")
		if err := o.legacy(nil); err != nil {
			o.log("%v", err)
		}
		return
	}
	note := ""
	if m.Version != o.Version {
		note = fmt.Sprintf(" (this binary is %s; run csquad update)", o.Version)
	}
	o.log("Installed:   csquad %s%s at %s", m.Version, note, m.Binary)
	var rels []string
	for rel := range m.Files {
		rels = append(rels, rel)
	}
	sort.Strings(rels)
	for _, rel := range rels {
		b, err := os.ReadFile(filepath.Join(o.ClaudeDir, filepath.FromSlash(rel)))
		state := "ok"
		switch {
		case err != nil:
			state = "missing"
		case sha(b) != m.Files[rel]:
			state = "edited"
		}
		o.log("  %-8s %s", state, rel)
	}
	o.statusCodex(m)
	if m.Notify != nil {
		o.log("Notifications: %s · %s", notify.Mode(o.Env, o.Home), m.Notify.Backend)
	}
	d, exists, err := loadDocument(o.settingsFile())
	if err != nil {
		o.log("Status line: unknown (%v)", err)
		return
	}
	var current struct {
		Command string `json:"command"`
	}
	if exists && d.value("statusLine", &current) && IsOurs(current.Command) {
		saved, _ := LoadSaved(o.Home)
		orig := "none"
		if c := saved.CommandLine(); c != "" {
			orig = strconv.Quote(c)
		} else if saved.Present {
			orig = string(saved.StatusLine)
		}
		o.log("Status line: wrapped (your original: %s)", orig)
	} else {
		o.log("Status line: not wrapped")
	}
	o.warnProjectOverride()
	o.warnClaudeVersion()
}

// warnProjectOverride: a project-level statusLine wins over the user-level
// wrapper.
func (o Options) warnProjectOverride() {
	if o.Cwd == "" {
		return
	}
	for _, name := range []string{"settings.json", "settings.local.json"} {
		f := filepath.Join(o.Cwd, ".claude", name)
		if filepath.Dir(f) == filepath.Clean(o.ClaudeDir) {
			continue // the config dir itself is user-level
		}
		var s map[string]any
		if readJSON(f, &s) && s["statusLine"] != nil {
			o.log("Warning: %s sets statusLine and overrides the csquad wrapper in this project.", f)
		}
	}
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

// warnClaudeVersion: finish needs `claude rm --discard-unpushed`.
func (o Options) warnClaudeVersion() {
	c, err := proc.Resolve("claude", []string{"--version"}, o.Env)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := c.Command(ctx)
	cmd.Env = o.Env
	out, _ := cmd.Output()
	m := versionRe.FindStringSubmatch(string(out))
	if m == nil {
		return
	}
	var v [3]int
	for i := range v {
		v[i], _ = strconv.Atoi(m[i+1])
	}
	if older(v, MinClaude) {
		o.log("Warning: Claude Code %d.%d.%d is older than %d.%d.%d; csquad finish cannot use claude rm --discard-unpushed there. Run `claude update`.", v[0], v[1], v[2], MinClaude[0], MinClaude[1], MinClaude[2])
	}
}

// older reports whether version a is older than b.
func older(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}
