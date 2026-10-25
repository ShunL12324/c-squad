//go:build !windows

package notify

// SystemRegistry is nil outside Windows.
func SystemRegistry() Registry { return nil }
