//go:build windows

package notify

import (
	"fmt"
	"os"
	"testing"
)

// The real registry, in a test key of its own: never the app ID's key.
func TestSystemRegistry(t *testing.T) {
	key := fmt.Sprintf(`Software\csquad-test\%d`, os.Getpid())
	reg := SystemRegistry()
	t.Cleanup(func() {
		reg.DeleteKey(key)
		reg.DeleteKey(`Software\csquad-test`)
	})
	if _, ok, err := reg.Values(key); ok || err != nil {
		t.Fatalf("the test key exists: %v", err)
	}
	testRegistryAppID(t, reg, key)
}
