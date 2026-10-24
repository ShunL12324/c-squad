// Package flock is an OS file lock (flock on Unix, LockFileEx on Windows):
// the lock goes with the process however it ends, so a lock that is free
// means nobody is working on what it guards.
package flock

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ErrHeld is returned when another process holds the lock.
var ErrHeld = errors.New("the lock is held by another process")

// Lock is a held lock.
type Lock struct{ f *os.File }

// Try takes the lock at path without waiting, creating the file (and its
// directory) when missing.
func Try(path string) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f}, nil
}

// Wait takes the lock, retrying until it is free or patience runs out.
func Wait(path string, patience time.Duration) (*Lock, error) {
	deadline := time.Now().Add(patience)
	for {
		l, err := Try(path)
		if !errors.Is(err, ErrHeld) || !time.Now().Before(deadline) {
			return l, err
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// Held reports whether another process holds the lock at path. A missing
// file is a free lock.
func Held(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	l, err := Try(path)
	if err != nil {
		return errors.Is(err, ErrHeld)
	}
	l.Release()
	return false
}

// Release frees the lock; it may be called more than once.
func (l *Lock) Release() {
	if l == nil || l.f == nil {
		return
	}
	unlock(l.f)
	l.f.Close()
	l.f = nil
}
