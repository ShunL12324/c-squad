package squad

import (
	"slices"
	"testing"
)

func TestWorkspaceConfirmationMatchesDirectoryAndDialog(t *testing.T) {
	screen := "Accessing workspace:\n\n /tmp/project\n\n ❯ No, exit\n   Yes, I trust this folder\n Enter to confirm"
	if !slices.Equal(claudeTrustKeys(screen, "/tmp/project"), []string{"Down"}) {
		t.Fatal("workspace prompt was not confirmed")
	}
	for _, bad := range []string{"/tmp/other", "/tmp", "/tmp/project/nested"} {
		if len(claudeTrustKeys(screen, bad)) != 0 {
			t.Fatal("confirmed another directory")
		}
	}
	if len(claudeTrustKeys("Allow this command? Yes, I trust this folder", "/tmp/project")) != 0 {
		t.Fatal("matched a tool approval")
	}
}
