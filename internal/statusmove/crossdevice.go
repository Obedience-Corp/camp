package statusmove

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func crossDeviceMove(src, dst string) error {
	if err := os.Rename(src, dst); err == nil {
		return nil
	} else if !isCrossDevice(err) {
		if os.IsExist(err) {
			return ErrAlreadyExists
		}
		return camperrors.Wrapf(err, "rename %s -> %s", src, dst)
	}
	return copyThenDelete(src, dst)
}

func copyThenDelete(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return camperrors.Wrapf(err, "stat %s", src)
	}

	removeOnFailure := false
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		removeOnFailure, err = copySymlinkNoReplace(src, dst)
	case info.IsDir():
		removeOnFailure, err = copyDirNoReplace(src, dst, info.Mode().Perm())
	default:
		removeOnFailure, err = copyFileNoReplace(src, dst, info.Mode().Perm())
	}
	if err != nil {
		return err
	}
	if info.IsDir() {
		if err := makeDirTreeWritableForRemoval(src); err != nil {
			if removeOnFailure {
				_ = os.RemoveAll(dst)
			}
			return camperrors.Wrapf(err, "prepare source removal %s", src)
		}
	}
	if err := os.RemoveAll(src); err != nil {
		if removeOnFailure {
			_ = os.RemoveAll(dst)
		}
		return camperrors.Wrapf(err, "remove source %s", src)
	}
	return nil
}

func makeDirTreeWritableForRemoval(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode().Perm()
		writable := mode | 0700
		if writable == mode {
			return nil
		}
		return os.Chmod(path, writable)
	})
}

func copyFileNoReplace(src, dst string, mode os.FileMode) (bool, error) {
	in, err := os.Open(src)
	if err != nil {
		return false, camperrors.Wrapf(err, "open %s", src)
	}
	defer func() {
		_ = in.Close()
	}()

	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode)
	if err != nil {
		if os.IsExist(err) {
			return false, ErrAlreadyExists
		}
		return false, camperrors.Wrapf(err, "create %s", dst)
	}
	removeOnFailure := true

	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return false, camperrors.Wrapf(err, "copy %s -> %s", src, dst)
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return false, camperrors.Wrapf(err, "sync %s", dst)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return false, camperrors.Wrapf(err, "close %s", dst)
	}
	return removeOnFailure, nil
}

func copyDirNoReplace(src, dst string, mode os.FileMode) (bool, error) {
	if err := os.Mkdir(dst, writableDirMode(mode)); err != nil {
		if os.IsExist(err) {
			return false, ErrAlreadyExists
		}
		return false, camperrors.Wrapf(err, "create directory %s", dst)
	}
	removeOnFailure := true

	entries, err := os.ReadDir(src)
	if err != nil {
		_ = os.RemoveAll(dst)
		return false, camperrors.Wrapf(err, "read directory %s", src)
	}
	for _, entry := range entries {
		childSrc := filepath.Join(src, entry.Name())
		childDst := filepath.Join(dst, entry.Name())
		if err := copyEntryNoReplace(childSrc, childDst); err != nil {
			_ = os.RemoveAll(dst)
			return false, err
		}
	}
	if err := os.Chmod(dst, mode); err != nil {
		_ = os.RemoveAll(dst)
		return false, camperrors.Wrapf(err, "restore directory mode %s", dst)
	}
	return removeOnFailure, nil
}

func writableDirMode(mode os.FileMode) os.FileMode {
	return mode | 0700
}

func copyEntryNoReplace(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return camperrors.Wrapf(err, "stat %s", src)
	}
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		_, err = copySymlinkNoReplace(src, dst)
	case info.IsDir():
		_, err = copyDirNoReplace(src, dst, info.Mode().Perm())
	default:
		_, err = copyFileNoReplace(src, dst, info.Mode().Perm())
	}
	return err
}

func copySymlinkNoReplace(src, dst string) (bool, error) {
	target, err := os.Readlink(src)
	if err != nil {
		return false, camperrors.Wrapf(err, "read symlink %s", src)
	}
	if err := os.Symlink(target, dst); err != nil {
		if os.IsExist(err) {
			return false, ErrAlreadyExists
		}
		return false, camperrors.Wrapf(err, "create symlink %s", dst)
	}
	return true, nil
}

func isCrossDevice(err error) bool {
	if err == nil {
		return false
	}
	if linkErr, ok := err.(*os.LinkError); ok {
		return linkErr.Err == syscall.EXDEV
	}
	return err == syscall.EXDEV
}
