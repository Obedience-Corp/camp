package artifacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SHA-256 is the manifest content hash. Nothing else in camp standardizes a
// content hash to defer to, and the record exists for external verification:
// a backup tool restoring a root checks it with plain `shasum -a 256`, which
// no faster non-cryptographic hash offers. The cost only shows on the first
// pass; steady state re-hashes nothing.

// HashManifest fills m's content hashes, re-reading only what moved.
//
// prev is the previous manifest for this root, nil on the first pass. An entry
// whose size, nanosecond mtime, and kind all match prev inherits prev's hash
// without a read, so the steady-state cost of a multi-hundred-GB root is a
// stat walk. Everything else is read and hashed.
//
// progress, when non-nil, receives the cumulative bytes hashed, at least once
// per chunk, so a worker heartbeat stays live through a first pass over a
// large file rather than being declared stale mid-read.
//
// A file that vanished between the walk and the read keeps an empty hash
// rather than failing the manifest: the record is of what the walk saw, and
// the next manifest will not include the file at all.
func HashManifest(ctx context.Context, campaignRoot string, m *Manifest, prev *Manifest, progress func(bytesHashed int64)) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var base map[string]FileEntry
	if prev != nil {
		base = prev.Index()
	}
	rootAbs := filepath.Join(campaignRoot, filepath.FromSlash(m.Root))

	var done int64
	for i := range m.Files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		entry := &m.Files[i]
		if entry.Symlink {
			continue
		}
		if b, ok := base[entry.Path]; ok && sameEntry(b, *entry) && b.HashSHA256 != "" {
			entry.HashSHA256 = b.HashSHA256
			continue
		}
		sum, err := hashFile(ctx, filepath.Join(rootAbs, filepath.FromSlash(entry.Path)), &done, progress)
		if os.IsNotExist(err) {
			entry.HashSHA256 = ""
			continue
		}
		if err != nil {
			return err
		}
		entry.HashSHA256 = sum
	}
	return nil
}

// hashFile reads one file through SHA-256, reporting cumulative progress per
// chunk via done/progress.
func hashFile(ctx context.Context, path string, done *int64, progress func(int64)) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", err
		}
		return "", camperrors.Wrapf(err, "open %s for hashing", path)
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	buf := make([]byte, 1<<20)
	for {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = h.Write(buf[:n])
			*done += int64(n)
			if progress != nil {
				progress(*done)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", camperrors.Wrapf(readErr, "read %s for hashing", path)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// committedManifest is the committed serialization shape. GeneratedAt is
// deliberately absent: it stamps every build, which is right for
// machine-local derived state and wrong for a committed record, where it
// would churn the file on every commit with nothing changed.
//
// DescribesCommit is the SHA whose artifact state the manifest captures. The
// carrier commit is usually later, because the manifest is written by a
// deferred job; recording the described commit explicitly keeps the record
// correct no matter when it lands. When contents have not changed the file is
// not rewritten at all, so the field keeps naming the commit that last
// changed the root, and an unchanged root stays byte-identical.
type committedManifest struct {
	Version         int         `json:"version"`
	Root            string      `json:"root"`
	DescribesCommit string      `json:"describes_commit"`
	Files           []FileEntry `json:"files"`
}

// CommittedJSON is the manifest's committed serialization: deterministic
// bytes that are identical whenever the contents and described commit are
// identical.
func (m *Manifest) CommittedJSON(describesCommit string) ([]byte, error) {
	committed := committedManifest{
		Version:         m.Version,
		Root:            m.Root,
		DescribesCommit: describesCommit,
		Files:           m.Files,
	}
	data, err := json.MarshalIndent(committed, "", "  ")
	if err != nil {
		return nil, camperrors.Wrapf(err, "encode committed manifest for %s", m.Root)
	}
	return append(data, '\n'), nil
}
