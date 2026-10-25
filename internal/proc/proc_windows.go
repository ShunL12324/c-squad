//go:build windows

package proc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func setCmdLine(cmd *exec.Cmd, line string) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.CmdLine = line
}

func hideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
}

func detach(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	// CREATE_NEW_PROCESS_GROUP | CREATE_NO_WINDOW: a console of its own
	// without a window, which the git, shells and agents it starts share. Not
	// DETACHED_PROCESS: Windows ignores CREATE_NO_WINDOW with it, and each
	// console program the process starts then opens a new console window.
	cmd.SysProcAttr.CreationFlags |= 0x00000200 | 0x08000000
	cmd.SysProcAttr.HideWindow = true
}

// killGroup: cancelling kills cmd's process tree. Windows has no process
// group a kill reaches; taskkill /T follows the parent ids.
func killGroup(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		killTree(cmd.Process.Pid)
		return cmd.Process.Kill()
	}
}

// killTree kills pid and every process it started (taskkill /T /F).
func killTree(pid int) {
	exe := "taskkill.exe"
	if root := os.Getenv("SystemRoot"); root != "" {
		exe = filepath.Join(root, "System32", "taskkill.exe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, exe, "/T", "/F", "/PID", strconv.Itoa(pid))
	hideWindow(cmd)
	_ = cmd.Run()
}
