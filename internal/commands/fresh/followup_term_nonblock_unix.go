//go:build !windows

package fresh

import (
	"os"
	"syscall"
)

func setFollowUpPTYNonblocking(master *os.File) error {
	return syscall.SetNonblock(int(master.Fd()), true)
}
