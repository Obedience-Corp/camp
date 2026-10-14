//go:build windows

package dirlink

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/windows"
)

func createPlatform(opts CreateOptions) error {
	needsSymlink, err := targetNeedsDirectorySymlink(opts.TargetPath)
	if err != nil {
		return err
	}
	if needsSymlink {
		return createDirectorySymlink(opts)
	}
	if err := createJunction(opts.LinkPath, opts.TargetPath); err != nil {
		return err
	}
	return nil
}

func createDirectorySymlink(opts CreateOptions) error {
	target := opts.StoreTarget
	if target == "" {
		target = opts.TargetPath
	}
	linkp, err := syscall.UTF16PtrFromString(fixLongPath(opts.LinkPath))
	if err != nil {
		return camperrors.Wrap(err, "directory symlink path")
	}
	// CreateSymbolicLink must receive the directory flag even when a network
	// target is temporarily unreachable. os.Symlink probes the target first
	// and creates a file symlink when that probe fails.
	storedTarget := target
	if filepath.IsAbs(target) {
		storedTarget = fixLongPath(target)
	}
	targetp, err := syscall.UTF16PtrFromString(storedTarget)
	if err != nil {
		return camperrors.Wrap(err, "directory symlink target")
	}
	// This Windows flag is present in Go's os.Symlink but absent from the
	// project's current x/sys/windows version.
	const symbolicLinkFlagAllowUnprivilegedCreate = 0x2
	flags := uint32(syscall.SYMBOLIC_LINK_FLAG_DIRECTORY | symbolicLinkFlagAllowUnprivilegedCreate)
	if err := syscall.CreateSymbolicLink(linkp, targetp, flags); err != nil {
		// The unprivileged flag is unsupported by older Windows releases.
		if retryErr := syscall.CreateSymbolicLink(linkp, targetp, syscall.SYMBOLIC_LINK_FLAG_DIRECTORY); retryErr != nil {
			return camperrors.Wrap(retryErr, directorySymlinkHint())
		}
	}
	return nil
}

func directorySymlinkHint() string {
	return "create directory symlink for this target (enable Windows Developer Mode or run elevated; camp does not copy project trees as a fallback)"
}

type reparseDataBufferHeader struct {
	ReparseTag        uint32
	ReparseDataLength uint16
	Reserved          uint16
}

type mountPointReparseBuffer struct {
	SubstituteNameOffset uint16
	SubstituteNameLength uint16
	PrintNameOffset      uint16
	PrintNameLength      uint16
	PathBuffer           [1]uint16
}

type reparseDataBuffer struct {
	header reparseDataBufferHeader
	detail [windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE]byte
}

func createJunction(link, target string) error {
	target = filepath.Clean(target)
	if !filepath.IsAbs(target) {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "junction target must be absolute")
	}
	if err := os.Mkdir(link, 0o755); err != nil {
		return camperrors.Wrap(err, "create junction directory")
	}

	substitute := `\??\` + target
	printName := target
	buf, buflen, err := mountPointBuffer(substitute, printName)
	if err != nil {
		_ = os.Remove(link)
		return err
	}

	var rdb reparseDataBuffer
	rdb.header.ReparseTag = ReparseTagMountPoint
	rdb.header.ReparseDataLength = buflen
	copy(rdb.detail[:], buf)

	if err := setReparsePoint(link, &rdb); err != nil {
		_ = os.Remove(link)
		return camperrors.Wrap(err, "set junction reparse point")
	}
	return nil
}

func mountPointBuffer(substitute, printName string) ([]byte, uint16, error) {
	subUTF16 := syscall.StringToUTF16(substitute)
	printUTF16 := syscall.StringToUTF16(printName)
	pathBuf := append(subUTF16, printUTF16...)

	var mp *mountPointReparseBuffer
	bufsize := int(unsafe.Offsetof(mp.PathBuffer)) + len(pathBuf)*2
	if bufsize > windows.MAXIMUM_REPARSE_DATA_BUFFER_SIZE-int(unsafe.Sizeof(reparseDataBufferHeader{})) {
		return nil, 0, camperrors.Wrap(camperrors.ErrInvalidInput, "junction target exceeds Windows reparse buffer limit")
	}
	buflen := uint16(bufsize)
	byteblob := make([]byte, buflen)
	mp = (*mountPointReparseBuffer)(unsafe.Pointer(&byteblob[0]))
	mp.SubstituteNameOffset = 0
	mp.SubstituteNameLength = uint16(len(subUTF16)-1) * 2
	mp.PrintNameOffset = uint16(len(subUTF16) * 2)
	mp.PrintNameLength = uint16(len(printUTF16)-1) * 2
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(&mp.PathBuffer[0])), len(pathBuf)), pathBuf)
	return byteblob, buflen, nil
}

func setReparsePoint(link string, rdb *reparseDataBuffer) error {
	link = fixLongPath(link)
	linkp, err := syscall.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	fd, err := syscall.CreateFile(linkp, syscall.GENERIC_WRITE, 0, nil, syscall.OPEN_EXISTING,
		syscall.FILE_FLAG_OPEN_REPARSE_POINT|syscall.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(fd)

	buflen := uint32(rdb.header.ReparseDataLength) + uint32(unsafe.Sizeof(rdb.header))
	var bytesReturned uint32
	err = syscall.DeviceIoControl(fd, windows.FSCTL_SET_REPARSE_POINT,
		(*byte)(unsafe.Pointer(&rdb.header)), buflen, nil, 0, &bytesReturned, nil)
	if err != nil {
		return camperrors.Wrap(err, "DeviceIoControl FSCTL_SET_REPARSE_POINT")
	}
	return nil
}
