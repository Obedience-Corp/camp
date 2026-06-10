package intent

import (
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// resolveByID returns the file path of the intent whose id: frontmatter equals
// id. Identity lives in the frontmatter, not the filename, so a renamed file
// (whose slug drifted from its id) still resolves.
//
// Resolution order, never a full per-lookup directory walk:
//  1. Fast path: stat the expected <id>.md across the intent status dirs. For
//     the common case (filename == id, no rename) this is O(statuses), the same
//     cost as before.
//  2. Index fallback: a lazily-built id->path map from a single directory pass,
//     cached on the service and invalidated on mutation.
func (s *IntentService) resolveByID(id string) (string, error) {
	for _, status := range AllStatuses() {
		p := s.getIntentPath(status, id)
		if it, err := s.loadIntent(p); err == nil && it.ID == id {
			return p, nil
		}
	}

	s.idIndexMu.Lock()
	defer s.idIndexMu.Unlock()
	if s.idIndex == nil {
		if err := s.buildIDIndexLocked(); err != nil {
			return "", err
		}
	}
	if p, ok := s.idIndex[id]; ok {
		return p, nil
	}
	return "", camperrors.Wrap(ErrNotFound, id)
}

// buildIDIndexLocked scans the intent status directories once and maps each
// intent's id: to its path. The caller must hold idIndexMu.
func (s *IntentService) buildIDIndexLocked() error {
	index := make(map[string]string)
	for _, status := range AllStatuses() {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return camperrors.Wrapf(err, "reading directory %s", dir)
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, f.Name())
			it, err := s.loadIntent(p)
			if err != nil || it.ID == "" {
				continue
			}
			index[it.ID] = p
		}
	}
	s.idIndex = index
	return nil
}

// invalidateIDIndex drops the cached id->path index so it rebuilds on the next
// fast-path miss. Called after any mutation that adds, moves, or removes a file.
func (s *IntentService) invalidateIDIndex() {
	s.idIndexMu.Lock()
	s.idIndex = nil
	s.idIndexMu.Unlock()
}
