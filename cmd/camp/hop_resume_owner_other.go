//go:build !unix

package main

import "os"

// hopFileOwner cannot read ownership here, so no path a hop's environment
// names is trusted and a hopped shell stays where it is.
func hopFileOwner(os.FileInfo) (int, bool) { return 0, false }

// hopNoFollowFlag is zero where the platform has no symlink-open flag.
func hopNoFollowFlag() int { return 0 }
