//go:build windows

package shellcmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const (
	powerShellTreeStopTimeout = 3 * time.Second
	powerShellWaitDelay       = 4 * time.Second
)

// configurePowerShellCancellation stops the interpreter and descendants when
// its context ends. Killing only powershell.exe can leave a native child
// running after the flow has reported cancellation.
func configurePowerShellCancellation(cmd *exec.Cmd) {
	cmd.Cancel = func() error {
		stopCtx, cancel := context.WithTimeout(context.Background(), powerShellTreeStopTimeout)
		defer cancel()
		stop := exec.CommandContext(stopCtx, "taskkill.exe", "/PID", strconv.Itoa(cmd.Process.Pid), "/T", "/F")
		output, err := stop.CombinedOutput()
		if err == nil {
			return nil
		}
		// End the interpreter even if taskkill could not walk the tree. Keep
		// the tree-stop failure visible even if Wait reports the killed
		// interpreter's exit status instead of the Cancel error.
		killErr := cmd.Process.Kill()
		stopErr := camperrors.Wrapf(err, "stopping PowerShell process tree: %s", strings.TrimSpace(string(output)))
		_, _ = fmt.Fprintln(os.Stderr, stopErr)
		if killErr != nil && !errors.Is(killErr, os.ErrProcessDone) {
			return errors.Join(stopErr, killErr)
		}
		return stopErr
	}
	cmd.WaitDelay = powerShellWaitDelay
}
