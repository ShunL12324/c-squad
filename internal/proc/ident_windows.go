package proc

import (
	"strconv"

	"golang.org/x/sys/windows"
)

// ident is the creation time of process pid; ok is false when no such
// process runs.
func ident(pid int) (string, bool) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if windows.GetExitCodeProcess(h, &code) != nil || code != 259 { // STILL_ACTIVE
		return "", false
	}
	var created, exited, kernel, user windows.Filetime
	if windows.GetProcessTimes(h, &created, &exited, &kernel, &user) != nil {
		return "", false
	}
	return strconv.FormatInt(created.Nanoseconds(), 10), true
}
