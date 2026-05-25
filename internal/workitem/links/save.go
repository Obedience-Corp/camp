package links

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// Save writes the registry to `links.yaml` under a file lock. Links are
// sorted in place before marshaling so the on-disk order is stable across
// runs.
//
// The directory `.campaign/workitems/` is created on demand. Writes are
// atomic via fsutil.WriteFileAtomically.
func Save(ctx context.Context, root string, links *Links) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if links == nil {
		return newValidation("links", "cannot save nil Links")
	}
	if links.Version == "" {
		links.Version = LinksSchemaVersion
	}
	if links.Links == nil {
		links.Links = []Link{}
	}
	links.Sort()

	data, err := marshalYAML(links)
	if err != nil {
		return camperrors.Wrap(err, "marshal links.yaml")
	}

	path := LinksPath(root)
	if err := os.MkdirAll(linksDir(root), 0o755); err != nil {
		return camperrors.Wrap(err, "create links dir")
	}

	release, err := acquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write links.yaml")
	}
	return nil
}

// SaveCurrent writes `current.yaml` under a file lock. Passing nil clears
// the file (removes it from disk); the directory is left in place.
func SaveCurrent(ctx context.Context, root string, c *Current) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := CurrentPath(root)

	if c == nil {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return camperrors.Wrap(err, "remove current.yaml")
		}
		return nil
	}

	if c.Version == "" {
		c.Version = CurrentSchemaVersion
	}

	data, err := marshalYAML(c)
	if err != nil {
		return camperrors.Wrap(err, "marshal current.yaml")
	}

	if err := os.MkdirAll(linksDir(root), 0o755); err != nil {
		return camperrors.Wrap(err, "create links dir")
	}

	release, err := acquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return camperrors.Wrap(err, "write current.yaml")
	}
	return nil
}

// marshalYAML encodes value as YAML with 2-space indent and a trailing
// newline.
func marshalYAML(value any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(value); err != nil {
		_ = enc.Close()
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// acquireFileLock takes an exclusive lock on lockPath via O_CREATE|O_EXCL.
// The returned release closure removes the lock file. Cross-platform; does
// not depend on flock(2). Times out after 5s with a wrapped error.
func acquireFileLock(ctx context.Context, lockPath string) (func(), error) {
	deadline := time.Now().Add(5 * time.Second)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err == nil {
			_ = f.Close()
			released := false
			return func() {
				if released {
					return
				}
				released = true
				_ = os.Remove(lockPath)
			}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, camperrors.Wrap(err, "acquire lock")
		}
		if time.Now().After(deadline) {
			return nil, camperrors.Wrap(camperrors.ErrInvalidInput,
				"timeout acquiring lock at "+lockPath+" (another camp invocation holds it?)")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(20 * time.Millisecond):
		}
	}
}
