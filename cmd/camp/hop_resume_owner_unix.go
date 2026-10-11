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
