package links

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"strconv"
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
	path := LinksPath(root)
	if err := os.MkdirAll(linksDir(root), 0o755); err != nil {
		return camperrors.Wrap(err, "create links dir")
	}
	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()
	return saveLocked(ctx, root, links)
}

// saveLocked marshals and writes the registry assuming the caller already
// holds links.yaml.lock. WithLock uses this so the Load->Mutate->Save window
// runs under a single lock acquisition.
func saveLocked(ctx context.Context, root string, links *Links) error {
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
	if err := fsutil.WriteFileAtomically(LinksPath(root), data, 0o644); err != nil {
		return camperrors.Wrap(err, "write links.yaml")
	}
	return nil
}

// WithLock holds links.yaml.lock for the full Load-Mutate-Save transaction
// so concurrent callers cannot silently drop each other's writes. fn receives
// the loaded registry; mutations are persisted on success. If fn returns
// ErrSkipSave the save is skipped (use this for "no-op after inspection").
func WithLock(ctx context.Context, root string, fn func(*Links) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(linksDir(root), 0o755); err != nil {
		return camperrors.Wrap(err, "create links dir")
	}
	path := LinksPath(root)
	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return err
	}
	defer release()

	registry, err := loadLocked(ctx, root)
	if err != nil {
		return err
	}
	if err := fn(registry); err != nil {
		if errors.Is(err, ErrSkipSave) {
			return nil
		}
		return err
	}
	return saveLocked(ctx, root, registry)
}

// ErrSkipSave signals to WithLock that the registry should not be persisted
// after the transaction. Use it when an inspection-only fn determines no
// mutation is needed.
var ErrSkipSave = errors.New("links: skip save")

// QuarantineBroken renames a malformed links.yaml to
// `links.yaml.broken-<unix-nano>` and writes a fresh empty registry in its
// place. The returned path is the quarantined file's new location (empty
// string if the original did not exist). Doctor's `--fix` uses this to
// unwedge a campaign whose registry cannot be loaded.
func QuarantineBroken(ctx context.Context, root string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if err := os.MkdirAll(linksDir(root), 0o755); err != nil {
		return "", camperrors.Wrap(err, "create links dir")
	}
	path := LinksPath(root)
	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
	if err != nil {
		return "", err
	}
	defer release()

	quarantined := ""
	if _, err := os.Stat(path); err == nil {
		quarantined = path + ".broken-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		if err := os.Rename(path, quarantined); err != nil {
			return "", camperrors.Wrap(err, "quarantine links.yaml")
		}
	}
	data, err := marshalYAML(Empty())
	if err != nil {
		return quarantined, camperrors.Wrap(err, "marshal empty registry")
	}
	if err := fsutil.WriteFileAtomically(path, data, 0o644); err != nil {
		return quarantined, camperrors.Wrap(err, "write empty registry")
	}
	return quarantined, nil
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

	release, err := fsutil.AcquireFileLock(ctx, path+".lock")
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
