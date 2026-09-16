//go:build !windows

package runner

import (
	"os/exec"
	"syscall"
)

// setProcessGroup puts the command in its own process group so that a signal
// reaches the whole tree (npm/bun → the actual dev server or watcher).
func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// interruptProcess sends SIGINT to the command's process group.
func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGINT)
}

// killProcess force-kills the command's process group.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}
