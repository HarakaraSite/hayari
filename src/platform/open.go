package platform

import (
	"os/exec"
	"runtime"
)

// OpenBrowser opens the given URL in the default browser.
func OpenBrowser(url string) {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default: // linux and others
		cmd = "xdg-open"
		args = []string{url}
	}

	// #nosec G204 -- cmd is selected from a fixed GOOS-specific allowlist.
	exec.Command(cmd, args...).Start()
}
