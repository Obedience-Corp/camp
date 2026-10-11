//go:build unix

package explore

import (
	"os"

	"golang.org/x/sys/unix"
)

func openRegular(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|unix.O_NONBLOCK|unix.O_NOFOLLOW, 0)
}
