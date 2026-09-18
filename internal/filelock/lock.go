package filelock

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

// Acquire obtains a process-shared advisory lock in dir/locks.
// With nonblock set, contention returns immediately. The returned release function
// is idempotent. Callers determine lock ordering; this package knows no team policy.
func Acquire(dir, name string, nonblock bool) (func(), error) {
	dir = filepath.Join(dir, "locks")
	if e := os.MkdirAll(dir, 0700); e != nil {
		return nil, e
	}
	f, e := os.OpenFile(filepath.Join(dir, name+".lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	op := syscall.LOCK_EX
	if nonblock {
		op |= syscall.LOCK_NB
	}
	if e = syscall.Flock(int(f.Fd()), op); e != nil {
		_ = f.Close()
		return nil, e
	}
	var once sync.Once
	return func() { once.Do(func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); _ = f.Close() }) }, nil
}
