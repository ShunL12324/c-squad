//go:build !windows

package proc

import (
	"os/exec"
	"syscall"
)

func setCmdLine(*exec.Cmd, string) {}

func hideWindow(*exec.Cmd) {}

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
}

// killTree kills the process group pid leads (KillGroup's children lead
// one), and pid itself should it lead none.
func killTree(pid int) {
	_ = syscall.Kill(-pid, syscall.SIGKILL)
	_ = syscall.Kill(pid, syscall.SIGKILL)
}
