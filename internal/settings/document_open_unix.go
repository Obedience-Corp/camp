//go:build unix

package settings

import (
	"os"

	"golang.org/x/sys/unix"
)

// regularOpenFlags opens a final file without following it and without
// blocking if a FIFO is substituted for that file.
func regularOpenFlags() int {
	return os.O_RDONLY | unix.O_NONBLOCK | unix.O_NOFOLLOW
}

// directoryOpenFlags opens a directory. O_DIRECTORY refuses a non-directory
// substituted at the final component.
func directoryOpenFlags() int {
	return os.O_RDONLY | unix.O_DIRECTORY | unix.O_NONBLOCK
}
