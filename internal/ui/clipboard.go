package ui

import (
	"os/exec"
	"runtime"
	"strings"
)

// WriteClipboard copies s to the system clipboard. Overridable in tests so
// they do not touch the real clipboard.
var WriteClipboard = func(s string) error {
	var c *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		c = exec.Command("pbcopy")
	default:
		c = exec.Command("xclip", "-selection", "clipboard")
	}
	c.Stdin = strings.NewReader(s)
	return c.Run()
}
