package explore

import (
	"context"
	"io"
	"io/fs"
	"os"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ReadRegular reads at most limit bytes from a regular, non-symlink file.
// Revalidate the opened file so replacement between discovery and open cannot
// turn a metadata read into a blocking pipe read or follow a different file.
func ReadRegular(ctx context.Context, path string, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "reading file")
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", path)
	}
	if !before.Mode().IsRegular() {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errNotRegular}
	}
	f, err := openRegular(path)
	if err != nil {
		return nil, camperrors.Wrapf(err, "opening %s", path)
	}
	defer func() { _ = f.Close() }()
	after, err := f.Stat()
	if err != nil {
		return nil, camperrors.Wrapf(err, "checking %s", path)
	}
	if !after.Mode().IsRegular() || !os.SameFile(before, after) {
		return nil, &fs.PathError{Op: "read", Path: path, Err: errFileChanged}
	}
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "reading file")
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return nil, camperrors.Wrapf(err, "reading %s", path)
	}
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "reading file")
	}
	return data, nil
}
