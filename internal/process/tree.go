package process

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Identity pairs a PID with its observed start time for safe lifecycle operations.
// Exported fields retain their persisted names for existing recovery snapshots.
type Identity struct {
	PID, PPID   int
	Stat, Start string
}

// Snapshot reads PID, parent PID, status, and start time using the system ps command.
// Start time is retained with each PID so later cleanup can detect PID reuse.
func Snapshot() (map[int]Identity, error) {
	out, e := Run("", "ps", "-axo", "pid=,ppid=,stat=,lstart=")
	if e != nil {
		return nil, e
	}
	all := map[int]Identity{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		pid, _ := strconv.Atoi(fields[0])
		ppid, _ := strconv.Atoi(fields[1])
		all[pid] = Identity{pid, ppid, fields[2], strings.Join(fields[3:], " ")}
	}
	return all, nil
}

// Descendants returns root and its transitive children from one process snapshot.
// It does not query the OS; callers must refresh before acting on these identities.
func Descendants(all map[int]Identity, root int) map[int]Identity {
	found := map[int]Identity{}
	if p, ok := all[root]; ok {
		found[root] = p
	}
	for {
		before := len(found)
		for id, p := range all {
			if _, ok := found[p.PPID]; ok {
				found[id] = p
			}
		}
		if before == len(found) {
			return found
		}
	}
}

// Alive reports whether a snapshot still contains the same non-zombie process.
// PID equality alone is insufficient because the OS may reuse a terminated PID.
func Alive(p Identity, all map[int]Identity) bool {
	current, ok := all[p.PID]
	return ok && current.Start == p.Start && !strings.HasPrefix(current.Stat, "Z")
}

// StopTree terminates an identified process tree, escalating from TERM to KILL.
// It refuses the current process and reused root PIDs. Callers must hold the
// member lifecycle lock to prevent a concurrent replacement during termination.
func StopTree(root int, start string) error {
	return stopTree(root, start, Snapshot)
}

// snapshot is injected at the OS boundary to exercise enumeration failures.
func stopTree(root int, start string, snapshot func() (map[int]Identity, error)) (err error) {
	if root <= 1 || root == os.Getpid() {
		return fmt.Errorf("invalid process root")
	}
	all, e := snapshot()
	if e != nil {
		return e
	}
	p, ok := all[root]
	if !ok {
		return nil
	}
	if start != "" && p.Start != start {
		return fmt.Errorf("process identity changed; refusing to signal PID %d", root)
	}
	tracked := Descendants(all, root)
	// A failed enumeration must not leave a previously frozen process suspended.
	// Refresh identities before thawing; a saved PID alone is unsafe after PID reuse.
	frozen := map[int]Identity{}
	defer func() {
		if len(frozen) == 0 {
			return
		}
		current, snapshotErr := snapshot()
		if snapshotErr != nil {
			err = errors.Join(err, fmt.Errorf("verify suspended processes: %w", snapshotErr))
			return
		}
		for _, identity := range frozen {
			if Alive(identity, current) {
				if resumeErr := syscall.Kill(identity.PID, syscall.SIGCONT); resumeErr != nil && !errors.Is(resumeErr, syscall.ESRCH) {
					err = errors.Join(err, fmt.Errorf("resume PID %d: %w", identity.PID, resumeErr))
				}
			}
		}
	}()

	// Freeze parents before enumeration/termination to prevent new descendants
	// from being spawned during handoff. SIGCONT follows SIGTERM so it is handled.
	for pass := 0; pass < 3; pass++ {
		for _, p := range tracked {
			if Alive(p, all) {
				if signalErr := syscall.Kill(p.PID, syscall.SIGSTOP); signalErr != nil {
					if errors.Is(signalErr, syscall.ESRCH) {
						continue
					}
					return fmt.Errorf("suspend PID %d: %w", p.PID, signalErr)
				}
				frozen[p.PID] = p
			}
		}
		all, e = snapshot()
		if e != nil {
			return e
		}
		for id, p := range Descendants(all, root) {
			tracked[id] = p
		}
	}
	for _, p := range tracked {
		if Alive(p, all) {
			_ = syscall.Kill(p.PID, syscall.SIGTERM)
			_ = syscall.Kill(p.PID, syscall.SIGCONT)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		all, e = snapshot()
		if e != nil {
			return e
		}
		count := 0
		for _, p := range tracked {
			if Alive(p, all) {
				count++
			}
		}
		if count == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	for _, p := range tracked {
		if Alive(p, all) {
			_ = syscall.Kill(p.PID, syscall.SIGKILL)
		}
	}
	time.Sleep(100 * time.Millisecond)
	all, e = snapshot()
	if e != nil {
		return e
	}
	for _, p := range tracked {
		if Alive(p, all) {
			return fmt.Errorf("PID %d still active; replacement refused", p.PID)
		}
	}
	return nil
}
