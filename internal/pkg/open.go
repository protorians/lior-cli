package pkg

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens url in the system default browser (best-effort, cross-
// platform). The process is started detached: the CLI never blocks on the
// browser and errors are reported by the caller.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}
