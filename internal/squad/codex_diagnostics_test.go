package squad

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"startup", "printf '%s\\n' 'Invalid configuration: duplicate key model' >&2; exit 23", "duplicate key model"},
		{"initialize", "read -r request; printf '%s\\n' '{\"id\":1,\"error\":{\"code\":-32600,\"message\":\"Unsupported client\"}}'; read -r request", "initialize failed (-32600): Unsupported client"},
		{"config", "read -r request; printf '%s\\n' '{\"id\":1,\"result\":{}}'; read -r ready; read -r request; printf '%s\\n' '{\"id\":2,\"error\":{\"code\":-32602,\"message\":\"Invalid project configuration\"}}'; read -r request", "config/read failed (-32602): Invalid project configuration"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			must(t, os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\n"+tc.script+"\n"), 0700))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			_, err := codexConfig(dir, false)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), dir) {
				t.Fatalf("missing diagnostics: %v", err)
			}
		})
	}
}

func TestCodexDiagnosticsBoundedAndRedacted(t *testing.T) {
	d := &codexDiagnostics{}
	_, _ = d.Write([]byte(strings.Repeat("x", 8000)))
	_, _ = d.Write([]byte("\napi_key = \"secret-value\"\nBearer private-value\nfailed sk-private123\nconfig.toml: duplicate key"))
	if len(d.tail) > 4096 {
		t.Fatal("unbounded diagnostics")
	}
	text := d.String()
	for _, secret := range []string{"secret-value", "private-value", "sk-private123"} {
		if strings.Contains(text, secret) {
			t.Fatal("credential included in diagnostics")
		}
	}
	if !strings.Contains(text, "config.toml: duplicate key") {
		t.Fatal("actionable error removed")
	}
}
