package proc

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Bounded makes cmd safe to time out: cancelling its context kills cmd with
// everything it started (SIGKILL to its own process group; its process tree
// on Windows), so a child that ignores SIGTERM goes too; and Wait stops
// waiting for output pipes a survivor still holds after delay.
func Bounded(cmd *exec.Cmd, delay time.Duration) {
	killGroup(cmd)
	cmd.WaitDelay = delay
}

// Tracked is a child csquad started and recorded in a directory, one file
// per child: a csquad that dies before reaping its child leaves the record,
// and a later Sweep acts on it.
type Tracked struct {
	PID int `json:"pid"`
	// Ident is the process's start time as the OS reports it: it tells the
	// child from a later process given the same pid.
	Ident string `json:"ident"`
	At    int64  `json:"at"` // when it started, unix ms
	By    string `json:"by"` // what started it, e.g. "statusline"
}

// Track records cmd, already started, in dir; the function returned forgets
// it. A record that cannot be written is skipped.
func Track(dir string, cmd *exec.Cmd, by string, now time.Time) func() {
	if dir == "" || cmd.Process == nil {
		return func() {}
	}
	pid := cmd.Process.Pid
	id, _ := ident(pid)
	b, _ := json.Marshal(Tracked{PID: pid, Ident: id, At: now.UnixMilli(), By: by})
	file := filepath.Join(dir, strconv.Itoa(pid)+".json")
	if os.MkdirAll(dir, 0o700) != nil || os.WriteFile(file, b, 0o600) != nil {
		return func() {}
	}
	return func() { os.Remove(file) }
}

// Sweep forgets the records of children that have exited, kills those that
// have run longer than limit along with everything they started, and returns
// the others: tracked children still running.
func Sweep(dir string, limit time.Duration, now time.Time) []Tracked {
	entries, _ := os.ReadDir(dir)
	var out []Tracked
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		file := filepath.Join(dir, e.Name())
		var t Tracked
		b, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		if json.Unmarshal(b, &t) != nil || t.PID <= 0 {
			os.Remove(file)
			continue
		}
		id, running := ident(t.PID)
		if !running || id != t.Ident {
			os.Remove(file) // exited; the pid may belong to another process now
			continue
		}
		if now.Sub(time.UnixMilli(t.At)) > limit {
			killTree(t.PID)
			os.Remove(file)
			continue
		}
		out = append(out, t)
	}
	return out
}

// Running reports whether process pid runs.
func Running(pid int) bool {
	_, ok := ident(pid)
	return ok
}
