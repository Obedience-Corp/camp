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

// HashOutcome reports what a hash pass could not pin down.
//
// Unsettled names the files whose bytes were still moving while they were read,
// so no single observation of them exists to record. Their entries carry the
// latest stat with an unknown hash; the caller surfaces them, because "the
// record could not describe these files" is operational news, not a detail.
type HashOutcome struct {
	Unsettled []string
}

// settleAttempts is how many times one file is re-read while it keeps moving
// under the reader. A file still changing after three passes is being actively
// written, and waiting longer inside a manifest job does not make it stop.
const settleAttempts = 3

// HashManifest fills m's content hashes, re-reading only what moved.
//
// prev is the previous manifest for this root, nil on the first pass. An entry
// whose size, nanosecond mtime, and kind all match prev inherits prev's hash
// without a read, so the steady-state cost of a multi-hundred-GB root is a
// stat walk. Everything else is read and hashed.
//
// Each hashed entry is re-statted around its read and takes the stat that
// describes the bytes actually hashed, which may be newer than the walk's. An
// entry whose size and mtime came from the walk but whose hash came from a read
// minutes later would describe a state that never existed at any single moment,
// and a record of a state that never existed is worse than no record.
//
// progress, when non-nil, receives the cumulative bytes hashed, at least once
// per chunk, so a worker heartbeat stays live through a first pass over a
// large file rather than being declared stale mid-read.
//
// A file that vanished between the walk and the read keeps an empty hash
// rather than failing the manifest: the record is of what the walk saw, and
// the next manifest will not include the file at all.
func HashManifest(ctx context.Context, campaignRoot string, m *Manifest, prev *Manifest, progress func(bytesHashed int64)) (*HashOutcome, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var base map[string]FileEntry
	if prev != nil {
		base = prev.Index()
	}
	rootAbs := filepath.Join(campaignRoot, filepath.FromSlash(m.Root))

	outcome := &HashOutcome{}
	var done int64
	for i := range m.Files {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		entry := &m.Files[i]
		if entry.Symlink {
			continue
		}
		if b, ok := base[entry.Path]; ok && sameEntry(b, *entry) && b.HashSHA256 != "" {
			entry.HashSHA256 = b.HashSHA256
			continue
		}
		read, err := hashSettledFile(ctx, filepath.Join(rootAbs, filepath.FromSlash(entry.Path)), &done, progress)
		if os.IsNotExist(err) {
			entry.HashSHA256 = ""
			continue
		}
		if err != nil {
			return nil, err
		}
		entry.Size, entry.MTime, entry.HashSHA256 = read.size, read.mtime, read.sum
		if !read.settled {
			outcome.Unsettled = append(outcome.Unsettled, entry.Path)
		}
	}
	return outcome, nil
}

// settledRead is one file's hash together with the stat that describes the
// bytes it was computed from.
type settledRead struct {
	sum     string
	size    int64
	mtime   int64
	settled bool
}

// hashSettledFile hashes path and returns the hash paired with a stat taken
// around the read, retrying while the file moves under the reader.
//
// When the file will not settle, the read returns the latest stat and NO hash.
// An unknown hash is a state the format already carries (a pre-hashing record
// unmarshals the same way) and the next pass re-reads it; a hash paired with a
// stat it does not belong to would be indistinguishable from a true record.
func hashSettledFile(ctx context.Context, path string, done *int64, progress func(int64)) (settledRead, error) {
	for attempt := 0; attempt < settleAttempts; attempt++ {
		if ctx.Err() != nil {
			return settledRead{}, ctx.Err()
		}
		before, err := os.Lstat(path)
		if err != nil {
			return settledRead{}, err
		}
		sum, err := hashFile(ctx, path, done, progress)
		if err != nil {
			return settledRead{}, err
		}
		after, err := os.Lstat(path)
		if err != nil {
			return settledRead{}, err
		}
		if before.Size() == after.Size() && before.ModTime().UnixNano() == after.ModTime().UnixNano() {
			return settledRead{sum: sum, size: after.Size(), mtime: after.ModTime().UnixNano(), settled: true}, nil
		}
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return settledRead{}, err
	}
	return settledRead{size: fi.Size(), mtime: fi.ModTime().UnixNano()}, nil
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
