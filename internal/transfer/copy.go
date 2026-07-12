package transfer

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// CopyFile copies a single file from src to dest, preserving permissions.
func CopyFile(src, dest string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return camperrors.Newf("open source: %w", err)
	}
	defer srcFile.Close()

	info, err := srcFile.Stat()
	if err != nil {
		return camperrors.Newf("stat source: %w", err)
	}

	destFile, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode())
	if err != nil {
		return camperrors.Newf("create destination: %w", err)
	}
	defer destFile.Close()

	if _, err := io.Copy(destFile, srcFile); err != nil {
		return camperrors.Newf("write data: %w", err)
	}
	return nil
}

// CopyDir recursively copies a directory from src to dest, preserving permissions.
func CopyDir(src, dest string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return camperrors.Newf("stat source: %w", err)
	}
	if !srcInfo.IsDir() {
		return camperrors.Newf("source is not a directory: %s", src)
	}

	if err := os.MkdirAll(dest, srcInfo.Mode()); err != nil {
		return camperrors.Newf("create destination directory: %w", err)
	}

	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return camperrors.Newf("compute relative path: %w", err)
		}
		target := filepath.Join(dest, rel)

		if d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return camperrors.Newf("stat directory: %w", err)
			}
			return os.MkdirAll(target, info.Mode())
		}

		return CopyFile(path, target)
	})
}
