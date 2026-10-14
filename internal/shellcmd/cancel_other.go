//go:build !windows

package shellcmd

import "os/exec"

func configurePowerShellCancellation(_ *exec.Cmd) {}
