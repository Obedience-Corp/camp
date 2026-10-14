//go:build windows

package dirlink

import (
	"os"
)

func statIrregular(path string, info os.FileInfo) (Stat, error) {
	if info.Mode()&os.ModeIrregular == 0 {
		return Stat{}, nil
	}
	tag, err := reparseTag(path)
	if err != nil {
		return Stat{}, err
	}
	st := Stat{ReparseTag: tag}
	switch tag {
	case ReparseTagSymlink:
		st.Kind = KindSymlink
	case ReparseTagMountPoint:
		st.Kind = KindJunction
	default:
		// ModeIrregular alone is not proof of a camp directory link (dedup,
		// appexeclink, etc.).
		st.Kind = KindNone
	}
	return st, nil
}
