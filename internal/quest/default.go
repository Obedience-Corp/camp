package quest

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// EnsureDefault returns the newest open fallback, creating one when its
// predecessors are closed. Historical identities and workspaces stay untouched.
// The lock covers selection and creation across concurrent CLI invocations.
func (s *Service) EnsureDefault(ctx context.Context) (*Quest, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	root := QuestsDir(s.campaignRoot)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, false, camperrors.Wrap(err, "create quests directory")
	}
	release, err := fsutil.AcquireFileLock(ctx, filepath.Join(root, ".default.lock"))
	if err != nil {
		return nil, false, camperrors.Wrap(err, "lock default quest creation")
	}
	defer release()
	scaffold, err := EnsureScaffold(ctx, s.campaignRoot)
	if err != nil {
		return nil, false, err
	}
	quests, err := List(ctx, s.campaignRoot, true)
	if err != nil {
		return nil, false, err
	}
	var current *Quest
	for _, q := range quests {
		if q.IsDefault() && q.Status == StatusOpen && (current == nil ||
			q.CreatedAt.After(current.CreatedAt) || (q.CreatedAt.Equal(current.CreatedAt) && q.ID > current.ID)) {
			current = q
		}
	}
	if current != nil {
		return current, slices.Contains(scaffold.CreatedFiles, current.Path), nil
	}
	metadata := DefaultQuest(time.Now().UTC())
	created, err := s.create(ctx, metadata.Name, metadata.Purpose, metadata.Description, nil, true)
	if err != nil {
		return nil, false, camperrors.Wrap(err, "create fresh default quest")
	}
	return created.Quest, true, nil
}
