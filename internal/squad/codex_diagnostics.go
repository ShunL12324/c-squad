package squad

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// codexDiagnostics retains a bounded stderr tail. Read it only after Cmd.Wait,
// when the subprocess output copier has finished.
type codexDiagnostics struct{ tail []byte }

func (d *codexDiagnostics) Write(p []byte) (int, error) {
	const limit = 4096
	n := len(p)
	if n >= limit {
		d.tail = append(d.tail[:0], p[n-limit:]...)
		return n, nil
	}
	if len(d.tail)+n > limit {
		d.tail = d.tail[len(d.tail)+n-limit:]
	}
	d.tail = append(d.tail, p...)
	return n, nil
}

var codexSecret = regexp.MustCompile(`(?i)sk-[a-z0-9_-]+|bearer\s+[^\s"']+|(?:api[_-]?key|access[_-]?token|refresh[_-]?token|password|secret)\s*["']?\s*[:=]\s*[^\r\n]+`)

func (d *codexDiagnostics) String() string {
	return codexSecret.ReplaceAllString(strings.TrimSpace(string(d.tail)), "[redacted]")
}

func codexRPCError(method string, raw json.RawMessage) error {
	var failure struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &failure) != nil {
		return fmt.Errorf("codex %s returned an invalid error response", method)
	}
	return fmt.Errorf("codex %s failed (%d): %s", method, failure.Code, codexSecret.ReplaceAllString(failure.Message, "[redacted]"))
}
