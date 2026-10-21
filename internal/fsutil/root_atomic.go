package fsutil

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"runtime"
)

// RootWriteError records a failure after an atomic replacement was applied.
type RootWriteError struct {
	Err     error
	Applied bool
}

func (e *RootWriteError) Error() string { return e.Err.Error() }
func (e *RootWriteError) Unwrap() error { return e.Err }

// WriteRootFileAtomically writes content to name inside root via a
// same-directory temp file and an atomic rename. If the destination already
// exists as a regular file, its permission bits are preserved; otherwise mode
// is used. The temp file is synced before the rename. After rename, the parent
// directory is synced when the platform supports it. Every create, stat,
// rename, and directory sync stays on root.
func WriteRootFileAtomically(root *os.Root, name string, content []byte, mode os.FileMode) error {
	return writeRootFile(root, name, content, mode, false)
}

// CreateRootFileAtomically publishes complete bytes only if name is absent.
func CreateRootFileAtomically(root *os.Root, name string, content []byte, mode os.FileMode) error {
	return writeRootFile(root, name, content, mode, true)
}

func writeRootFile(root *os.Root, name string, content []byte, mode os.FileMode, create bool) error {
	perm := mode.Perm()
	info, err := root.Lstat(name)
	if err == nil {
		if create {
			return &os.PathError{Op: "create", Path: name, Err: fs.ErrExist}
		}
		if !info.Mode().IsRegular() {
			return &os.PathError{Op: "write", Path: name, Err: fs.ErrInvalid}
		}
		perm = info.Mode().Perm()
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	tmpName := rootSibling(name, ".tmp-"+randomHex(8))
	tmpFile, err := root.OpenFile(tmpName, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return err
	}
	defer func() {
		_ = root.Remove(tmpName)
	}()

	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(perm); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Close(); err != nil {
		return err
	}

	if create {
		if err := root.Link(tmpName, name); err != nil {
			return err
		}
	} else {
		if err := root.Rename(tmpName, name); err != nil {
			return err
		}
	}
	if err := SyncRootDir(root, path.Dir(name)); err != nil {
		return &RootWriteError{Err: err, Applied: true}
	}
	return nil
}

// SyncRootDir fsyncs a directory inside root. On Windows, directory Sync is
// unsupported and this returns nil, matching SyncDir.
func SyncRootDir(root *os.Root, dir string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	if dir == "" || dir == "." {
		dir = "."
	}
	f, err := root.Open(dir)
	if err != nil {
		return err
	}
	syncErr := f.Sync()
	if closeErr := f.Close(); syncErr == nil {
		syncErr = closeErr
	}
	return syncErr
}
