//go:build !windows

package plugin

import (
	"os"
	"os/exec"
)

// configurePluginCancel asks the plugin to stop when its context ends, then
// bounds how long camp waits before Kill. Matches hop preparation: Interrupt
// first so journal/JSON finalize can complete; WaitDelay then Kill for a wedge.
func configurePluginCancel(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		return cmd.Process.Signal(os.Interrupt)
	}
	cmd.WaitDelay = pluginCancelGrace
}
