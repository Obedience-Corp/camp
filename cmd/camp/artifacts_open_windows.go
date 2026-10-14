//go:build windows

package main

import (
	"errors"
	"runtime"
	"syscall"
	"unsafe"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"golang.org/x/sys/windows"
)

var artifactShellExecute = windows.NewLazySystemDLL("shell32.dll").NewProc("ShellExecuteExW")

const (
	seeMaskNoAsync = 0x00000100
	seeMaskNoUI    = 0x00000400
	seeMaskUnicode = 0x00004000
)

// Layout of SHELLEXECUTEINFOW from shellapi.h. In particular, the DWORD
// fields are followed by pointer-aligned fields on both 32-bit and 64-bit
// Windows.
type artifactShellExecuteInfo struct {
	size       uint32
	mask       uint32
	window     windows.Handle
	verb       *uint16
	file       *uint16
	parameters *uint16
	directory  *uint16
	show       int32
	instance   windows.Handle
	idList     uintptr
	class      *uint16
	classKey   windows.Handle
	hotKey     uint32
	icon       windows.Handle
	process    windows.Handle
}

// artifactOpenWindows asks the Windows Shell to open a document with its
// registered handler. ShellExecuteExW returns a launch result, not a process to
// wait for; the selected application continues independently of the explorer.
var artifactOpenWindows = func(path string) error {
	file, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return camperrors.Wrap(err, "encode artifact path")
	}
	verb, err := windows.UTF16PtrFromString("open")
	if err != nil {
		return camperrors.Wrap(err, "encode Windows Shell verb")
	}

	// Shell extensions may use COM and require a single-threaded apartment.
	// The call and its COM lifetime must remain on the same OS thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED|windows.COINIT_DISABLE_OLE1DDE); err != nil && !errors.Is(err, syscall.Errno(1)) {
		return camperrors.Wrap(err, "initialize Windows Shell")
	}
	defer windows.CoUninitialize()

	info := artifactShellExecuteInfo{
		size: uint32(unsafe.Sizeof(artifactShellExecuteInfo{})),
		// The TUI reports errors itself, and this worker has no message loop.
		mask: seeMaskNoAsync | seeMaskNoUI | seeMaskUnicode,
		verb: verb,
		file: file,
		show: windows.SW_SHOWNORMAL,
	}
	ok, _, callErr := artifactShellExecute.Call(uintptr(unsafe.Pointer(&info)))
	if ok != 0 {
		return nil
	}
	return artifactShellError(uintptr(info.instance), callErr)
}

func artifactShellError(code uintptr, callErr error) error {
	if code == 0 && errors.Is(callErr, windows.ERROR_NO_ASSOCIATION) {
		code = 31
	}
	var reason string
	switch code {
	case 8:
		reason = "Windows has insufficient resources"
	case 2:
		reason = "file was not found"
	case 3:
		reason = "folder was not found"
	case 5:
		reason = "access was denied"
	case 11:
		reason = "application format is invalid"
	case 26:
		reason = "file is in use"
	case 27:
		reason = "file association is incomplete"
	case 28, 29, 30:
		reason = "associated application did not respond"
	case 31:
		reason = "no application is associated with this file type"
	case 32:
		reason = "associated application is missing a required library"
	default:
		reason = "Windows Shell could not launch the associated application"
	}
	if errno, ok := callErr.(syscall.Errno); ok && errno != 0 {
		return camperrors.Wrapf(callErr, "%s (ShellExecuteEx code %d)", reason, code)
	}
	return camperrors.Newf("%s (ShellExecuteEx code %d)", reason, code)
}
