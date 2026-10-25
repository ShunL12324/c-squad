package notify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
)

// File appends each notification to a file as a JSON line and shows
// nothing (CSQUAD_NOTIFY_FILE: tests and debugging).
type File struct{ Path string }

func (f File) Name() string { return "file " + f.Path }

func (f File) Send(_ context.Context, m Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o700); err != nil {
		return err
	}
	out, err := os.OpenFile(f.Path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := out.Write(append(b, '\n')); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
