//go:build unix

package main

import (
	"os"
	"syscall"
)

// hopFileOwner returns the uid that owns the file info describes.
func hopFileOwner(info os.FileInfo) (int, bool) {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(stat.Uid), true
}

// hopNoFollowFlag is or'd into OpenFile so a symlink at the final path
// component is an error instead of a file the caller did not name.
func hopNoFollowFlag() int { return syscall.O_NOFOLLOW }

// hopNonblockFlag is or'd into OpenFile so a fifo cannot block the open.
// O_NOFOLLOW does not reject one.
func hopNonblockFlag() int { return syscall.O_NONBLOCK }
