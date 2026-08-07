package triage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// AppendDecision appends one event to the run's verdict stream.
func (s *Store) AppendDecision(ctx context.Context, runID string, event DecisionEvent) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	line, err := MarshalLine(&event)
	if err != nil {
		return err
	}
	return s.appendLine(ctx, filepath.Join(s.RunDir(runID), DecisionsFileName), line)
}

// Decisions reads the run's verdict stream in append order.
func (s *Store) Decisions(ctx context.Context, runID string) ([]DecisionEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path := filepath.Join(s.RunDir(runID), DecisionsFileName)

	// Read under the stream's lock. An unlocked read can land between a
	// concurrent appender's write and its fsync and see a partial final line,
	// which would surface as a parse error on a file that is actually fine.
	var raw []byte
	err := withLock(ctx, lockPathFor(path), func() error {
		var readErr error
		raw, readErr = os.ReadFile(path)
		return readErr
	})
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrapf(err, "read %s", path)
	}

	var events []DecisionEvent
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event DecisionEvent
		if err := ParseDocument([]byte(line), &event, Lenient); err != nil {
			return nil, camperrors.Wrapf(err, "%s line %d", path, i+1)
		}
		events = append(events, event)
	}
	return events, nil
}

// Verdicts returns the current standing of every decided row.
func (s *Store) Verdicts(ctx context.Context, runID string) (map[string]RowVerdict, error) {
	events, err := s.Decisions(ctx, runID)
	if err != nil {
		return nil, err
	}
	return FoldDecisions(events), nil
}

// WriteEvidence stores an evidence record, reporting whether it changed
// anything. Re-submitting a byte-identical record is a no-op, so a driver that
// retries a batch does not churn the run; a record with different content
// replaces the previous one.
func (s *Store) WriteEvidence(ctx context.Context, runID string, record *EvidenceRecord) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if record == nil {
		return false, camperrors.NewValidation("record", "is required", nil)
	}
	body, err := MarshalDocument(record)
	if err != nil {
		return false, err
	}

	path := s.EvidencePath(runID, record.StableID)
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return false, camperrors.Wrapf(err, "create evidence directory for run %s", runID)
	}

	// Compare-then-write holds the lock across both halves, so the reported
	// "did this change anything" answer is the truth about what landed rather
	// than about what was there a moment earlier.
	written := false
	err = withLock(ctx, lockPathFor(path), func() error {
		existing, readErr := os.ReadFile(path)
		switch {
		case readErr == nil:
			if contentHash(existing) == contentHash(body) {
				return nil
			}
		case !errors.Is(readErr, fs.ErrNotExist):
			return camperrors.Wrapf(readErr, "read %s", path)
		}
		if writeErr := writeAtomic(path, body); writeErr != nil {
			return writeErr
		}
		written = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return written, nil
}

// Evidence loads one row's evidence record. A missing record is not an error:
// a row may legitimately have none yet.
func (s *Store) Evidence(ctx context.Context, runID, stableID string) (*EvidenceRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var record EvidenceRecord
	if err := s.readDocument(s.EvidencePath(runID, stableID), &record); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &record, nil
}

// EvidencePath is where one row's record lives.
func (s *Store) EvidencePath(runID, stableID string) string {
	return filepath.Join(s.RunDir(runID), EvidenceDirName, recordFileName(stableID))
}

// recordFileName maps a stable id to the filename its per-row records use
// (evidence and rationales both).
//
// Stable ids are slugs, but they arrive from markers camp did not necessarily
// write, so the mapping is an allowlist rather than a blocklist: everything
// outside [A-Za-z0-9_-] becomes a hyphen. Dots are included in that, which
// removes ".." as a construct entirely instead of relying on it being harmless
// once the separators are gone.
//
// Sanitizing is lossy, so it is not injective: "a/b" and "a-b" would both
// become "a-b". Two rows sharing an evidence file is silent data loss in an
// identity system, so any id that had to be rewritten gets a digest of the
// exact original appended. Ids that need no rewriting — every normal slug —
// keep their plain readable filename.
func recordFileName(stableID string) string {
	safe := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		case r == '-', r == '_':
			return r
		default:
			return '-'
		}
	}, stableID)

	if safe == stableID && strings.Trim(safe, "-") != "" {
		return safe + ".json"
	}
	suffix := contentHash([]byte(stableID))[:8]
	if strings.Trim(safe, "-") == "" {
		return "unnamed-" + suffix + ".json"
	}
	return safe + "-" + suffix + ".json"
}

// lockPathFor is the lock guarding path. Locks are named by the file they
// protect so two operations on the same file always contend on the same lock.
func lockPathFor(path string) string { return path + ".lock" }

// withLock runs fn while holding the lock at lockPath.
//
// Read-modify-write sequences must hold the lock across the whole sequence,
// not just the write: locking only the write would let two invocations both
// read the same state, both compute an update from it, and lose one.
func withLock(ctx context.Context, lockPath string, fn func() error) error {
	release, err := fsutil.AcquireFileLock(ctx, lockPath)
	if err != nil {
		return err
	}
	defer release()
	return fn()
}

// writeAtomic writes path via temp-and-rename. The caller must already hold
// path's lock.
func writeAtomic(path string, body []byte) error {
	if err := fsutil.WriteFileAtomically(path, body, fileMode); err != nil {
		return camperrors.Wrapf(err, "write %s", path)
	}
	return nil
}

// writeLocked writes path atomically while holding its lock. Use it for
// blind writes; a read-modify-write needs withLock around the whole sequence.
func (s *Store) writeLocked(ctx context.Context, path string, body []byte) error {
	return withLock(ctx, lockPathFor(path), func() error {
		return writeAtomic(path, body)
	})
}

// appendLine appends one line to a JSONL stream under its lock, flushing to
// disk before releasing so a reader that takes the lock next sees the record.
func (s *Store) appendLine(ctx context.Context, path string, line []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return camperrors.Wrapf(err, "create %s", filepath.Dir(path))
	}
	return withLock(ctx, lockPathFor(path), func() error {
		f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
		if err != nil {
			return camperrors.Wrapf(err, "open %s", path)
		}
		if _, err := f.Write(line); err != nil {
			_ = f.Close()
			return camperrors.Wrapf(err, "append to %s", path)
		}
		if err := f.Sync(); err != nil {
			_ = f.Close()
			return camperrors.Wrapf(err, "sync %s", path)
		}
		return camperrors.Wrapf(f.Close(), "close %s", path)
	})
}

// readDocument loads and validates one of the store's own files. Store reads
// are lenient about additive fields so a run written by a newer camp opens.
func (s *Store) readDocument(path string, doc Document) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return camperrors.Wrapf(err, "read %s", path)
	}
	if err := ParseDocument(raw, doc, Lenient); err != nil {
		return camperrors.Wrapf(err, "read %s", path)
	}
	return nil
}

// contentHash identifies an evidence record's bytes for the idempotency check.
func contentHash(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}
