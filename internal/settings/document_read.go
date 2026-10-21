package settings

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

const dirChunk = 32

func opCancelled(ctx context.Context, root, campID string) error {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	return &OpError{Code: CodeCancelled, Message: "operation cancelled", CampID: campID, CampRoot: root, Err: err}
}

func opIO(err error, root, campID, path string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &OpError{Code: CodeCancelled, Message: "operation cancelled", CampID: campID, CampRoot: root, Path: path, Err: err}
	}
	return &OpError{Code: CodeIO, Message: err.Error(), CampID: campID, CampRoot: root, Path: path, Err: camperrors.Wrap(err, "read document")}
}

func opInvalid(err error, root, campID, path string) error {
	message := ""
	if err != nil {
		message = err.Error()
	}
	var validation *camperrors.ValidationError
	if errors.As(err, &validation) && validation.Message != "" {
		message = validation.Message
	}
	if isEscape(err) {
		message = "path is not inside the campaign directory"
	}
	return &OpError{Code: CodeInvalidPath, Message: message, CampID: campID, CampRoot: root, Path: path, Err: err}
}

func kindOf(info fs.FileInfo) DocumentKind {
	mode := info.Mode()
	switch {
	case mode&os.ModeSymlink != 0:
		return KindSymlink
	case mode.IsDir():
		return KindDirectory
	case mode.IsRegular():
		return KindFile
	default:
		return KindSpecial
	}
}

// lstatContained Lstats each path component inside root. A symlink is visible
// only as the final component. Parent symlinks and paths that leave the root
// are rejected. os.Root still prevents a swapped parent from escaping.
func lstatContained(root *os.Root, rel string) (fs.FileInfo, error) {
	parts := strings.Split(rel, "/")
	current := ""
	var info fs.FileInfo
	for i, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, camperrors.NewValidation("path", "path is not inside the campaign directory", nil)
		}
		if current == "" {
			current = part
		} else {
			current = current + "/" + part
		}
		st, err := root.Lstat(current)
		if err != nil {
			return nil, err
		}
		if st.Mode()&os.ModeSymlink != 0 && i < len(parts)-1 {
			return nil, camperrors.NewValidation("path", "path is not inside the campaign directory", nil)
		}
		info = st
	}
	return info, nil
}

func openRegular(root *os.Root, name string) (*os.File, error) {
	f, err := root.OpenFile(name, regularOpenFlags(), 0)
	if err != nil {
		return nil, err
	}
	st, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !st.Mode().IsRegular() {
		_ = f.Close()
		return nil, camperrors.NewValidation("path", "document is not a regular file", nil)
	}
	return f, nil
}

func openDirectory(root *os.Root, name string) (*os.File, error) {
	if name == "" {
		name = "."
	}
	return root.OpenFile(name, directoryOpenFlags(), 0)
}

func readFileLimited(root *os.Root, name string, limit int) ([]byte, error) {
	f, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if limit >= 0 && st.Size() > int64(limit) {
		return nil, errOversized()
	}
	buf, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(buf) > limit {
		return nil, errOversized()
	}
	return buf, nil
}

func readPrefix(root *os.Root, name string, n int) ([]byte, error) {
	f, err := openRegular(root, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, int64(n)))
}

func directoryHasEntry(root *os.Root, name string) (bool, error) {
	f, err := openDirectory(root, name)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	entries, err := f.ReadDir(1)
	if len(entries) > 0 {
		return true, nil
	}
	if err == nil || errors.Is(err, io.EOF) {
		return false, nil
	}
	return false, err
}

// readManifestBytes reads campaign.yaml inside an anchored .campaign root.
// unreadable is true when the path exists but is not a regular file.
func readManifestBytes(root *os.Root, limit int) (source []byte, unreadable bool, err error) {
	const name = "campaign.yaml"
	info, err := root.Lstat(name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if kindOf(info) != KindFile {
		return nil, true, nil
	}
	if limit >= 0 && info.Size() > int64(limit) {
		return nil, false, errOversized()
	}
	source, err = readFileLimited(root, name, limit)
	return source, false, err
}

func errOversized() error {
	return &OpError{Code: CodeOversized, Message: "document exceeds the size limit"}
}

func isEscape(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && pathErr.Err != nil && pathErr.Err.Error() == "path escapes from parent"
}

func isNotDirectory(err error) bool {
	var pathErr *os.PathError
	return errors.As(err, &pathErr) && pathErr.Err != nil && strings.Contains(pathErr.Err.Error(), "not a directory")
}
