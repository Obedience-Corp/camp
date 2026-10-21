//go:build !unix

package settings

import "os"

func regularOpenFlags() int { return os.O_RDONLY }

func directoryOpenFlags() int { return os.O_RDONLY }
