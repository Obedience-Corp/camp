package ui

import (
	"os/exec"
	"runtime"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// OpenInBrowser hands url to the platform's URL opener. Overridable in tests so
// they never launch a real browser.
//
// Only http(s) is accepted. The URLs camp opens come from another process's
// stderr, and while the argument is passed as its own argv element (so there is
// no shell to inject into), `open` on macOS will happily act on a file path or
// an arbitrary scheme. Refusing anything that is not a web URL keeps a remote
// host from choosing what this launches.
var OpenInBrowser = func(url string) error {
	if !isWebURL(url) {
		return camperrors.New("refusing to open a non-http(s) url: " + url)
	}
	cmd := browserOpenCommand(url)
	if cmd == nil {
		return camperrors.New("no url opener known for " + runtime.GOOS)
	}
	if err := cmd.Start(); err != nil {
		return camperrors.Wrap(err, "opening "+url)
	}
	// Reap in the background: the opener is not ours to wait on (xdg-open can
	// live as long as the browser it spawned), but an unwaited child is a
	// zombie for the life of the TUI.
	go func() { _ = cmd.Wait() }()
	return nil
}

func browserOpenCommand(url string) *exec.Cmd {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url)
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url)
	default:
		return exec.Command("xdg-open", url)
	}
}

func isWebURL(url string) bool {
	return strings.HasPrefix(url, "https://") || strings.HasPrefix(url, "http://")
}
