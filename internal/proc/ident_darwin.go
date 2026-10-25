package proc

import (
	"strconv"

	"golang.org/x/sys/unix"
)

// ident is the start time of process pid; ok is false when no such process
// runs.
func ident(pid int) (string, bool) {
	k, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(k.Proc.P_pid) != pid || k.Proc.P_stat == 5 { // SZOMB: it has exited
		return "", false
	}
	t := k.Proc.P_starttime
	return strconv.FormatInt(int64(t.Sec), 10) + "." + strconv.FormatInt(int64(t.Usec), 10), true
}
