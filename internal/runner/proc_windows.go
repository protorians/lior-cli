//go:build windows

package runner

import "os/exec"

// setProcessGroup is a no-op on Windows (process groups are Unix-specific).
func setProcessGroup(cmd *exec.Cmd) {}

// interruptProcess terminates the command (Windows has no SIGINT delivery).
func interruptProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// killProcess force-kills the command.
func killProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
