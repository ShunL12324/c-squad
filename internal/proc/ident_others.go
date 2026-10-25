//go:build !linux && !darwin && !windows

package proc

import "syscall"

// ident: without a start time that tells a process from a later one with
// the same pid, only whether pid runs.
func ident(pid int) (string, bool) {
	return "", syscall.Kill(pid, 0) == nil
}
