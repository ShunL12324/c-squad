package install

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// settings.json is edited as text: only the bytes of the statusLine member
// change, so every other key keeps its order, spacing, line endings and
// final newline.

type member struct {
	key                string
	start, valStart    int // start: the key's opening quote
	valEnd             int
	prevEnd            int // end of the previous member's value (or after '{')
	nextStart, nextSep int // the next member's start (-1 if none), the comma after this value (-1 if none)
}

type document struct {
	raw     []byte
	open    int // index of '{'
	close   int // index of '}'
	members []member
	eol     string
	indent  string // one level; "" = compact
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }

func skipSpace(b []byte, i int) int {
	for i < len(b) && isSpace(b[i]) {
		i++
	}
	return i
}

// scanString returns the index after the string starting at b[i] == '"'.
func scanString(b []byte, i int) (int, error) {
	for j := i + 1; j < len(b); j++ {
		switch b[j] {
		case '\\':
			j++
		case '"':
			return j + 1, nil
		}
	}
	return 0, errors.New("unterminated string")
}

// scanValue returns the index after the JSON value starting at b[i].
func scanValue(b []byte, i int) (int, error) {
	if i >= len(b) {
		return 0, errors.New("unexpected end")
	}
	switch b[i] {
	case '"':
		return scanString(b, i)
	case '{', '[':
		depth := 0
		for j := i; j < len(b); j++ {
			switch b[j] {
			case '"':
				end, err := scanString(b, j)
				if err != nil {
					return 0, err
				}
				j = end - 1
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return j + 1, nil
				}
			}
		}
		return 0, errors.New("unterminated value")
	}
	j := i
	for j < len(b) && !isSpace(b[j]) && b[j] != ',' && b[j] != '}' && b[j] != ']' {
		j++
	}
	return j, nil
}

var memberIndent = regexp.MustCompile(`(?m)^([ \t]+)"`)

// parseDocument reads a settings file that must be a JSON object.
func parseDocument(raw []byte) (*document, error) {
	if !json.Valid(raw) {
		var v any
		err := json.Unmarshal(raw, &v)
		return nil, fmt.Errorf("not valid JSON (%v)", err)
	}
	d := &document{raw: raw, eol: "\n"}
	if bytes.Contains(raw, []byte("\r\n")) {
		d.eol = "\r\n"
	}
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '{' {
		return nil, errors.New("not a JSON object")
	}
	d.open = i
	prevEnd := i + 1
	i = skipSpace(raw, i+1)
	for i < len(raw) && raw[i] != '}' {
		m := member{start: i, prevEnd: prevEnd, nextStart: -1, nextSep: -1}
		end, err := scanString(raw, i)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw[i:end], &m.key); err != nil {
			return nil, err
		}
		i = skipSpace(raw, end)
		if raw[i] != ':' {
			return nil, errors.New("expected ':'")
		}
		m.valStart = skipSpace(raw, i+1)
		if m.valEnd, err = scanValue(raw, m.valStart); err != nil {
			return nil, err
		}
		prevEnd = m.valEnd
		i = skipSpace(raw, m.valEnd)
		if raw[i] == ',' {
			m.nextSep = i
			i = skipSpace(raw, i+1)
			m.nextStart = i
		}
		d.members = append(d.members, m)
	}
	d.close = i
	if mm := memberIndent.FindSubmatch(raw); mm != nil {
		d.indent = string(mm[1])
	} else if len(d.members) == 0 {
		d.indent = "  "
	}
	return d, nil
}

func (d *document) find(key string) (member, bool) {
	for _, m := range d.members {
		if m.key == key {
			return m, true
		}
	}
	return member{}, false
}

// value parses the value of key.
func (d *document) value(key string, v any) bool {
	m, ok := d.find(key)
	if !ok {
		return false
	}
	return json.Unmarshal(d.raw[m.valStart:m.valEnd], v) == nil
}

func (d *document) rawValue(key string) (string, bool) {
	m, ok := d.find(key)
	if !ok {
		return "", false
	}
	return string(d.raw[m.valStart:m.valEnd]), true
}

func splice(b []byte, from, to int, with string) []byte {
	out := make([]byte, 0, len(b)-(to-from)+len(with))
	out = append(out, b[:from]...)
	out = append(out, with...)
	return append(out, b[to:]...)
}

// set replaces key's value with text (already formatted for depth 1), or
// adds the member after the last one.
func (d *document) set(key, text string) []byte {
	if m, ok := d.find(key); ok {
		return splice(d.raw, m.valStart, m.valEnd, text)
	}
	k, _ := json.Marshal(key)
	if d.indent == "" {
		if len(d.members) == 0 {
			return splice(d.raw, d.open+1, d.close, string(k)+":"+text)
		}
		last := d.members[len(d.members)-1]
		return splice(d.raw, last.valEnd, last.valEnd, ","+string(k)+":"+text)
	}
	if len(d.members) == 0 {
		return splice(d.raw, d.open+1, d.close, d.eol+d.indent+string(k)+": "+text+d.eol)
	}
	last := d.members[len(d.members)-1]
	return splice(d.raw, last.valEnd, last.valEnd, ","+d.eol+d.indent+string(k)+": "+text)
}

// remove deletes key's member with one separator.
func (d *document) remove(key string) []byte {
	m, ok := d.find(key)
	if !ok {
		return d.raw
	}
	switch {
	case m.prevEnd > d.open+1: // not the first: from the previous value's end
		return splice(d.raw, m.prevEnd, m.valEnd, "")
	case m.nextStart >= 0: // the first of several: up to the next member
		return splice(d.raw, m.start, m.nextStart, "")
	default: // the only member
		return splice(d.raw, d.open+1, d.close, "")
	}
}

// format writes v as JSON at depth 1 in the document's style, keys in the
// given order.
func (d *document) format(v orderedObject) string {
	if d.indent == "" {
		return v.compact()
	}
	return v.pretty(d.indent, 1, d.eol)
}

type kv struct {
	key string
	val any
}

// orderedObject is a JSON object whose keys keep their order.
type orderedObject []kv

func marshal(v any) string {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return strings.TrimRight(b.String(), "\n")
}

func (o orderedObject) compact() string {
	parts := make([]string, len(o))
	for i, p := range o {
		parts[i] = marshal(p.key) + ":" + marshal(p.val)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func (o orderedObject) pretty(indent string, depth int, eol string) string {
	if len(o) == 0 {
		return "{}"
	}
	inner := strings.Repeat(indent, depth+1)
	parts := make([]string, len(o))
	for i, p := range o {
		val := marshal(p.val)
		if obj, ok := p.val.(orderedObject); ok {
			val = obj.pretty(indent, depth+1, eol)
		} else if m, ok := p.val.(map[string]any); ok {
			b, _ := json.MarshalIndent(m, strings.Repeat(indent, depth+1), indent)
			val = strings.ReplaceAll(string(b), "\n", eol)
		}
		parts[i] = inner + marshal(p.key) + ": " + val
	}
	return "{" + eol + strings.Join(parts, ","+eol) + eol + strings.Repeat(indent, depth) + "}"
}

// ---- files and links -------------------------------------------------------

func isLink(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode()&fs.ModeSymlink != 0
}

// resolveLink follows a chain of symlinks to the path a write should land
// on, also when the last link is dangling.
func resolveLink(file string) (string, error) {
	p := file
	for i := 0; i < 40 && isLink(p); i++ {
		target, err := os.Readlink(p)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(p), target)
		}
		p = target
	}
	if isLink(p) {
		return "", fmt.Errorf("%s is a symlink loop", file)
	}
	return p, nil
}

// renameRetry renames over to. On Windows a rename fails with a sharing or
// access error while another process (Claude Code, an antivirus scanner) has
// the target open, so it retries for a while with a growing pause.
func renameRetry(from, to string) error {
	var err error
	for i := 1; i <= 12; i++ {
		if err = os.Rename(from, to); err == nil || runtime.GOOS != "windows" || !retryable(err) {
			return err
		}
		time.Sleep(time.Duration(min(20*i, 250)) * time.Millisecond)
	}
	return err
}

func retryable(err error) bool {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		// ERROR_ACCESS_DENIED, ERROR_SHARING_VIOLATION, ERROR_LOCK_VIOLATION
		return errno == 5 || errno == 32 || errno == 33
	}
	return errors.Is(err, fs.ErrPermission)
}

// writeAtomic writes through a temp file and rename, following a symlinked
// target (dotfile setups, dangling ones included) and keeping its mode.
func writeAtomic(file string, data []byte, mode fs.FileMode) error {
	target, err := resolveLink(file)
	if err != nil {
		return err
	}
	if st, err := os.Stat(target); err == nil {
		mode = st.Mode().Perm()
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	tmp := target + ".csquad-" + strconv.Itoa(os.Getpid()) + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := renameRetry(tmp, target); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
