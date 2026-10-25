package proc

import (
	"os/exec"
	"regexp"
)

// baseKeys is what programs csquad starts in the background need: paths,
// the user and locale, proxies, the SSH agent, the agents' own config
// directories, csquad's own variables, the desktop's session bus and WSL's
// interop (the notifications), and Windows' system variables.
var baseKeys = regexp.MustCompile(`^(PATH|HOME|USER|LOGNAME|SHELL|TMPDIR|TMP|TEMP|LANG|LANGUAGE|LC_[A-Z_]+|TZ|` +
	`(?i:http_proxy|https_proxy|no_proxy|all_proxy)|SSH_AUTH_SOCK|XDG_[A-Z_]+|CLAUDE_CONFIG_DIR|CODEX_HOME|CSQUAD_[A-Z0-9_]+|` +
	`DBUS_SESSION_BUS_ADDRESS|WSL_DISTRO_NAME|WSL_INTEROP|` +
	`(?i:SystemRoot|SystemDrive|windir|ComSpec|PATHEXT|USERPROFILE|APPDATA|LOCALAPPDATA|ProgramData|ProgramFiles|ProgramFiles\(x86\)|` +
	`ProgramW6432|CommonProgramFiles|CommonProgramFiles\(x86\)|CommonProgramW6432|HOMEDRIVE|HOMEPATH|USERNAME|USERDOMAIN|COMPUTERNAME|` +
	`NUMBER_OF_PROCESSORS|PROCESSOR_ARCHITECTURE|OS|CLAUDE_CODE_GIT_BASH_PATH))$`)

// Base is e reduced to an allowlist: what a detached csquad process (and
// the reviewers, checks, git and sessions it starts) runs with. A Stop hook
// runs with the environment the worker's settings injected (provider
// tokens among it); none of that reaches what csquad starts from there.
func (e Env) Base() Env {
	return e.Without(func(k string) bool { return !baseKeys.MatchString(k) })
}

// Detach makes cmd outlive csquad: its own session (process group on
// Windows), no console window, no stdin.
func Detach(cmd *exec.Cmd) {
	cmd.Stdin = nil
	detach(cmd)
}

// KillGroup makes cancelling cmd's context kill everything it started, not
// only cmd (a shell's children keep its output open otherwise).
func KillGroup(cmd *exec.Cmd) { killGroup(cmd) }
