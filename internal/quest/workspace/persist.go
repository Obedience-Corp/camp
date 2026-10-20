package workspace

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// Store reads and writes quest ui-state.json under the shared OS lock contract.
type Store struct {
	LockWait time.Duration
}

// Read loads ui-state.json bytes and generation. Missing file yields empty
// version-2 document bytes and the generation of those empty bytes.
func (s *Store) Read(path string) (raw []byte, generation string, err error) {
	raw, err = os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			empty := emptyDocument()
			return empty, Generation(empty), nil
		}
		return nil, "", camperrors.Wrap(err, "read ui-state.json")
	}
	return raw, Generation(raw), nil
}

// Push validates the spec, acquires ui-state.lock, merges, checks generation,
// and atomically replaces the file. On validation failure nothing is written.
func (s *Store) Push(ctx context.Context, campaignRoot, uiStatePath string, spec *PushSpec) (*MergePushResult, string, error) {
	if err := ValidatePushSpec(campaignRoot, spec); err != nil {
		return nil, "", err
	}
	if err := os.MkdirAll(filepath.Dir(uiStatePath), 0o755); err != nil {
		return nil, "", camperrors.Wrap(err, "create quest directory for ui-state")
	}

	wait := s.LockWait
	if wait <= 0 {
		wait = DefaultLockWait
	}
	release, err := acquireOSLock(ctx, LockPath(uiStatePath), wait)
	if err != nil {
		return nil, "", err
	}
	defer release()

	prior, priorGen, err := s.Read(uiStatePath)
	if err != nil {
		return nil, "", err
	}
	if len(bytesTrimSpace(prior)) > 0 {
		if err := ValidateDocument(campaignRoot, prior); err != nil {
			return nil, "", camperrors.Wrap(err, "existing ui-state.json invalid")
		}
	}

	merged, err := MergePush(prior, spec)
	if err != nil {
		return nil, "", err
	}

	// Detect a legacy writer that raced during merge without holding the lock.
	latest, latestGen, err := s.Read(uiStatePath)
	if err != nil {
		return nil, "", err
	}
	_ = latest
	if latestGen != priorGen {
		return nil, "", camperrors.Wrap(camperrors.ErrConflict,
			"PERSISTENCE_CONFLICT: ui-state.json changed during merge; retry")
	}

	if err := fsutil.WriteFileAtomically(uiStatePath, merged.Document, 0o644); err != nil {
		return nil, "", camperrors.Wrap(err, "write ui-state.json")
	}
	return merged, Generation(merged.Document), nil
}

func emptyDocument() []byte {
	return []byte("{\n  \"version\": 2,\n  \"workspaceTabs\": []\n}")
}
