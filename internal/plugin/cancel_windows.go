//go:build windows

package plugin

import (
	"os/exec"
)

// configurePluginCancel bounds plugin teardown on Windows. os.Interrupt is not
// delivered to arbitrary children, so Cancel kills the process and WaitDelay
// still caps how long Wait blocks on a stuck pipe after Kill.
func configurePluginCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = pluginCancelGrace
}
