package pkg

import (
	"bytes"
	"fmt"
	"os/exec"
)

// HasCommand reports whether an executable is available in the PATH.
func HasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

// StreamCommand runs an external command and reports the exit error.
func StreamCommand(name string, args ...string) error {
	return StreamCommandIn("", name, args...)
}

// StreamCommandIn runs an external command inside dir and reports the error.
func StreamCommandIn(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("command %q failed: %v — %s", name, err, stderr.String())
	}
	return nil
}
