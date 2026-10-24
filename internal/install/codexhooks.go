package install

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ShunL12324/c-squad/assets"
)

// Codex runs hooks from $CODEX_HOME/hooks.json. csquad adds one Stop group,
// marked "__csquad__": true, last in the list, and edits the file as text:
// the user's groups keep their bytes, and an unchanged csquad group stays
// where it is, so the trust Codex keeps per file, event and position holds.

func (o Options) codexHooksFile() string { return filepath.Join(o.codexHome(), "hooks.json") }

// CodexHookGroup is csquad's Stop group for the binary and home.
func CodexHookGroup(binary, home string) string {
	win := runtime.GOOS == "windows"
	command := assets.ShellPath(binary, win) + " hook --home " + assets.ShellPath(home, win) + " codex-stop"
	c, _ := json.Marshal(command)
	return `{"__csquad__": true, "hooks": [{"type": "command", "command": ` + string(c) + `, "timeout": 30}]}`
}

type jsonMember struct {
	key             string
	start, valStart int
	valEnd          int
}

// objectAt reads the members of the object whose '{' is at b[open]; it
// returns them and the index of the closing '}'.
func objectAt(b []byte, open int) ([]jsonMember, int, error) {
	var out []jsonMember
	i := skipSpace(b, open+1)
	for i < len(b) && b[i] != '}' {
		end, err := scanString(b, i)
		if err != nil {
			return nil, 0, err
		}
		var key string
		if err := json.Unmarshal(b[i:end], &key); err != nil {
			return nil, 0, err
		}
		j := skipSpace(b, end)
		if j >= len(b) || b[j] != ':' {
			return nil, 0, errors.New("expected ':'")
		}
		vs := skipSpace(b, j+1)
		ve, err := scanValue(b, vs)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, jsonMember{key, i, vs, ve})
		i = skipSpace(b, ve)
		if i < len(b) && b[i] == ',' {
			i = skipSpace(b, i+1)
		}
	}
	if i >= len(b) {
		return nil, 0, errors.New("unterminated object")
	}
	return out, i, nil
}

// arrayAt reads the elements ([start, end) pairs) of the array at b[open].
func arrayAt(b []byte, open int) ([][2]int, int, error) {
	var out [][2]int
	i := skipSpace(b, open+1)
	for i < len(b) && b[i] != ']' {
		end, err := scanValue(b, i)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, [2]int{i, end})
		i = skipSpace(b, end)
		if i < len(b) && b[i] == ',' {
			i = skipSpace(b, i+1)
		}
	}
	if i >= len(b) {
		return nil, 0, errors.New("unterminated array")
	}
	return out, i, nil
}

func memberOf(ms []jsonMember, key string) (jsonMember, bool) {
	for _, m := range ms {
		if m.key == key {
			return m, true
		}
	}
	return jsonMember{}, false
}

// leading is the whitespace before the first member or element after the
// bracket at open, used to separate an added one like the others; def when
// the container is empty or compact.
func leading(b []byte, open int, def string) string {
	i := skipSpace(b, open+1)
	if ws := string(b[open+1 : i]); ws != "" {
		return ws
	}
	return def
}

func marked(b []byte, el [2]int) bool {
	if b[el[0]] != '{' {
		return false
	}
	var g map[string]any
	if json.Unmarshal(b[el[0]:el[1]], &g) != nil {
		return false
	}
	v, _ := g["__csquad__"].(bool)
	return v
}

// addCodexHook returns raw with group in hooks.Stop and the text csquad's
// part of the file now is (the manifest's hooksText): what it inserted, a
// whole file it created, or prev (what an earlier install inserted) when the
// group was already there, with the group replaced when it changed. It is ""
// only when prev is unknown and the group was there already.
func addCodexHook(raw []byte, group, prev string) ([]byte, string, error) {
	if strings.TrimSpace(string(raw)) == "" {
		file := "{\n  \"hooks\": {\n    \"Stop\": [\n      " + group + "\n    ]\n  }\n}\n"
		return []byte(file), file, nil
	}
	if !json.Valid(raw) {
		return nil, "", errors.New("not valid JSON")
	}
	top := skipSpace(raw, 0)
	if raw[top] != '{' {
		return nil, "", errors.New("not a JSON object")
	}
	insert := func(at int, text string) ([]byte, string, error) {
		return splice(raw, at, at, text), text, nil
	}
	// A member added to the object at open: after its last member, or
	// first in an empty one.
	addMember := func(open int, ms []jsonMember, def, text string) ([]byte, string, error) {
		if len(ms) == 0 {
			return insert(open+1, text)
		}
		return insert(ms[len(ms)-1].valEnd, ","+leading(raw, open, def)+text)
	}
	ms, _, err := objectAt(raw, top)
	if err != nil {
		return nil, "", err
	}
	hooks, ok := memberOf(ms, "hooks")
	if !ok {
		return addMember(top, ms, "\n  ", `"hooks": {"Stop": [`+group+`]}`)
	}
	if raw[hooks.valStart] != '{' {
		return nil, "", errors.New(`"hooks" is not an object`)
	}
	hs, _, err := objectAt(raw, hooks.valStart)
	if err != nil {
		return nil, "", err
	}
	stop, ok := memberOf(hs, "Stop")
	if !ok {
		return addMember(hooks.valStart, hs, " ", `"Stop": [`+group+`]`)
	}
	if raw[stop.valStart] != '[' {
		return nil, "", errors.New(`"hooks.Stop" is not a list`)
	}
	els, _, err := arrayAt(raw, stop.valStart)
	if err != nil {
		return nil, "", err
	}
	for _, el := range els {
		if marked(raw, el) {
			current := string(raw[el[0]:el[1]])
			if !strings.Contains(string(raw), prev) || !strings.Contains(prev, current) {
				prev = ""
			}
			if current == group {
				return raw, prev, nil
			}
			return splice(raw, el[0], el[1], group), strings.Replace(prev, current, group, 1), nil
		}
	}
	if len(els) == 0 {
		return insert(stop.valStart+1, group)
	}
	return insert(els[len(els)-1][1], ","+leading(raw, stop.valStart, " ")+group)
}

// removeCodexHook takes csquad's group out of raw: exactly the text install
// inserted when the file still holds it, else every marked group with its
// separator.
func removeCodexHook(raw []byte, inserted string) ([]byte, bool) {
	if inserted != "" {
		if i := strings.Index(string(raw), inserted); i >= 0 && strings.Count(string(raw), inserted) == 1 {
			return splice(raw, i, i+len(inserted), ""), true
		}
	}
	changed := false
	for {
		next, ok := removeMarked(raw)
		if !ok {
			return raw, changed
		}
		raw, changed = next, true
	}
}

func removeMarked(raw []byte) ([]byte, bool) {
	if !json.Valid(raw) {
		return raw, false
	}
	top := skipSpace(raw, 0)
	if top >= len(raw) || raw[top] != '{' {
		return raw, false
	}
	ms, _, err := objectAt(raw, top)
	if err != nil {
		return raw, false
	}
	hooks, ok := memberOf(ms, "hooks")
	if !ok || raw[hooks.valStart] != '{' {
		return raw, false
	}
	hs, _, err := objectAt(raw, hooks.valStart)
	if err != nil {
		return raw, false
	}
	stop, ok := memberOf(hs, "Stop")
	if !ok || raw[stop.valStart] != '[' {
		return raw, false
	}
	els, _, err := arrayAt(raw, stop.valStart)
	if err != nil {
		return raw, false
	}
	for k, el := range els {
		if !marked(raw, el) {
			continue
		}
		switch {
		case k > 0:
			return splice(raw, els[k-1][1], el[1], ""), true
		case len(els) > 1:
			return splice(raw, el[0], els[1][0], ""), true
		}
		return splice(raw, el[0], el[1], ""), true
	}
	return raw, false
}

// installCodexHook adds csquad's Stop group to Codex's hooks.json when
// Codex is installed ($CODEX_HOME exists), and records csquad's text in it.
func (o Options) installCodexHook(m *Manifest, old Manifest) error {
	m.HooksText = old.HooksText
	dir := o.codexHome()
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		if old.HooksText != "" {
			o.uninstallCodexHook(old)
			m.HooksText = ""
		}
		return nil
	}
	file := o.codexHooksFile()
	raw, err := os.ReadFile(file)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	next, text, err := addCodexHook(raw, CodexHookGroup(BinaryPath(o.Home), o.Home), old.HooksText)
	if err != nil {
		return fmt.Errorf("%s: %v; fix it or move it away, then run install again", file, err)
	}
	m.HooksText = text
	if string(next) == string(raw) {
		return nil
	}
	if err := writeAtomic(file, next, 0o644); err != nil {
		return err
	}
	o.log("Codex: Stop hook added to %s; approve it once in Codex with /hooks (until then csquad notices finished Codex turns on its next command)", file)
	return nil
}

// uninstallCodexHook takes csquad's group out of Codex's hooks.json; a file
// left empty (csquad created it) goes.
func (o Options) uninstallCodexHook(m Manifest) {
	file := o.codexHooksFile()
	raw, err := os.ReadFile(file)
	if err != nil {
		return
	}
	next, changed := removeCodexHook(raw, m.HooksText)
	if !changed {
		return
	}
	if strings.TrimSpace(string(next)) == "" {
		err = os.Remove(file)
	} else {
		err = writeAtomic(file, next, 0o644)
	}
	if err != nil {
		o.log("Codex: could not update %s (%v); remove the group marked \"__csquad__\" by hand", file, err)
		return
	}
	o.log("Codex: Stop hook removed from %s", file)
}
