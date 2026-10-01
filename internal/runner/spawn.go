package runner

import "os/exec"

// Spawn places the command in its own process group (so a signal reaches the
// whole tree — npm/bun → the actual dev server) and starts it.
func Spawn(cmd *exec.Cmd) error {
	setProcessGroup(cmd)
	return cmd.Start()
}

// InterruptGroup politely interrupts the command's whole process group
// (SIGINT on Unix, termination on Windows).
func InterruptGroup(cmd *exec.Cmd) {
	interruptProcess(cmd)
}

// KillGroup force-kills the command's whole process group.
func KillGroup(cmd *exec.Cmd) {
	killProcess(cmd)
}
