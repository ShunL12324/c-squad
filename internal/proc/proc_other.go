//go:build !windows

package proc

import "os/exec"

func setCmdLine(*exec.Cmd, string) {}

func hideWindow(*exec.Cmd) {}
