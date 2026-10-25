package proc

import (
	"os"
	"strconv"
	"strings"
)

// ident is the start time of process pid in clock ticks since boot
// (/proc/<pid>/stat, field 22); ok is false when no such process runs.
func ident(pid int) (string, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", false
	}
	// The command name, field 2, is in parentheses and may hold spaces.
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	if i < 0 {
		return "", false
	}
	f := strings.Fields(s[i+1:])
	if len(f) < 20 || f[0] == "Z" {
		return "", false // a zombie has exited
	}
	return f[19], true
}
