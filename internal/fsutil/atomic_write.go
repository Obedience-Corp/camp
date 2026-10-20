// Package fsutil holds small, dependency-free filesystem helpers shared across
// internal packages.
package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
)

// WriteFileAtomically writes content to path via a same-directory temp file
// and an atomic rename. If the destination already exists, its permission
// bits are preserved; otherwise defaultMode is used. The temp file is fsync'd
// before the rename so crashes cannot leave behind half-written content.
// After rename, the parent directory is fsync'd when the platform supports it
// so the directory entry itself is durable.
func WriteFileAtomically(path string, content []byte, defaultMode os.FileMode) error {
	mode := defaultMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}

	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()
	defer func() {
		_ = os.Remove(tmpPath)
	}()

	if _, err := tmpFile.Write(content); err != nil {
		_ = tmpFile.Close()
		return err
	}
	if err := tmpFile.Chmod(mode); err != nil {
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

	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return SyncDir(dir)
}

// SyncDir fsyncs a directory so recent renames/creates are durable in the
// directory entry. On Windows, directory Sync is unsupported; SyncDir is a
// successful no-op there while still propagating Open failures.
func SyncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := f.Sync(); err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return err
	}
	return nil
}
