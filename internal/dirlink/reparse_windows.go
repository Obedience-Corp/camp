//go:build windows

package dirlink

import (
	"path/filepath"
	"syscall"
	"unsafe"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/windows"
)

type fileAttributeTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

func reparseTag(path string) (uint32, error) {
	path = fixLongPath(path)
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return 0, camperrors.Wrap(err, "reparse tag path")
	}
	h, err := syscall.CreateFile(p, 0, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, camperrors.Wrap(err, "open reparse point")
	}
	defer syscall.CloseHandle(h)

	var ti fileAttributeTagInfo
	err = windows.GetFileInformationByHandleEx(windows.Handle(h), windows.FileAttributeTagInfo,
		(*byte)(unsafe.Pointer(&ti)), uint32(unsafe.Sizeof(ti)))
	if err != nil {
		return 0, camperrors.Wrap(err, "read reparse tag")
	}
	if ti.FileAttributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT == 0 {
		return 0, nil
	}
	return ti.ReparseTag, nil
}

func targetNeedsDirectorySymlink(absTarget string) (bool, error) {
	if uncPath(absTarget) {
		return true, nil
	}
	vol := filepath.VolumeName(filepath.Clean(absTarget))
	if vol == "" {
		return false, nil
	}
	root := vol + `\`
	p, err := syscall.UTF16PtrFromString(root)
	if err != nil {
		return false, camperrors.Wrap(err, "drive root path")
	}
	switch windows.GetDriveType(p) {
	case windows.DRIVE_REMOTE:
		return true, nil
	default:
		return false, nil
	}
}

// fixLongPath adds the \\?\ prefix for absolute paths longer than MAX_PATH,
// matching the os package on Windows.
func fixLongPath(path string) string {
	if len(path) < 4 {
		return path
	}
	if path[:4] == `\\?\` {
		return path
	}
	if len(path) < 260 {
		return path
	}
	if vol := filepath.VolumeName(path); vol != "" {
		if len(path)-len(vol) >= 260 {
			return `\\?\` + path
		}
	} else if len(path) >= 260 {
		return `\\?\` + path
	}
	return path
}
