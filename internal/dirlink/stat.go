package dirlink

import (
	"os"
)

func statFromLstat(path string, info os.FileInfo) (Stat, error) {
	if info.Mode()&os.ModeSymlink != 0 {
		return Stat{Kind: KindSymlink}, nil
	}
	return statIrregular(path, info)
}
