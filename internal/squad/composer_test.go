package squad

import (
	"strings"
	"testing"
)

func TestCodexComposerAfterResume(t *testing.T) {
	for _, tt := range []struct {
		name, pane string
		ready      bool
	}{
		{"resumed without banner", "Previous result\n› Ask Codex to do anything\nmodel · project", true},
		{"typed input", "› Please inspect this code\nmodel · project", false},
		{"working", "Working (esc to interrupt)\n› Ask Codex to do anything\nmodel", false},
		{"historical prompt", "› Ask Codex to do anything\n" + strings.Repeat("old output\n", 20), false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if codexEmptyComposer(tt.pane) != tt.ready {
				t.Fatal("incorrect composer readiness")
			}
		})
	}
}
