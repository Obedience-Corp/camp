package sync

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// predictURLChanges returns URL changes that would happen during sync.
func (s *Syncer) predictURLChanges(preflight *PreflightResult) []URLChange {
	changes := make([]URLChange, len(preflight.URLMismatches))
	for i, mismatch := range preflight.URLMismatches {
		changes[i] = URLChange{
			Submodule: mismatch.Submodule,
			OldURL:    mismatch.ActiveURL,
			NewURL:    mismatch.DeclaredURL,
		}
	}
	return changes
}

// syncURLs runs git submodule sync to copy URLs from .gitmodules to .git/config.
func (s *Syncer) syncURLs(ctx context.Context) ([]URLChange, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Capture URL state before sync
	beforeURLs, err := s.captureURLs(ctx)
	if err != nil {
		return nil, &SyncError{Op: "capture-urls-before", Cause: err}
	}

	// Run git submodule sync --recursive
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"submodule", "sync", "--recursive")
	if output, err := cmd.CombinedOutput(); err != nil {
		return nil, &SyncError{Op: "submodule-sync", Cause: camperrors.Wrapf(git.ErrSubmoduleSync, "%s", strings.TrimSpace(string(output)))}
	}

	// Capture URL state after sync
	afterURLs, err := s.captureURLs(ctx)
	if err != nil {
		return nil, &SyncError{Op: "capture-urls-after", Cause: err}
	}

	// Compute what changed
	return s.diffURLs(beforeURLs, afterURLs), nil
}

// captureURLs captures the current URL state for all submodules.
func (s *Syncer) captureURLs(ctx context.Context) (map[string]string, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	urls := make(map[string]string)
	for _, path := range paths {
		url, err := git.GetActiveURL(ctx, s.repoRoot, path)
		if err != nil {
			continue // Skip submodules with URL issues
		}
		urls[path] = url
	}

	return urls, nil
}

// diffURLs computes URL changes between before and after states.
func (s *Syncer) diffURLs(before, after map[string]string) []URLChange {
	var changes []URLChange

	for path, afterURL := range after {
		beforeURL := before[path]
		if beforeURL != afterURL && beforeURL != "" {
			changes = append(changes, URLChange{
				Submodule: path,
				OldURL:    beforeURL,
				NewURL:    afterURL,
			})
		}
	}

	return changes
}

// reverseSyncLocalURLs detects submodules where .gitmodules has a local
// filesystem path but the submodule has a real remote origin configured,
// and updates .gitmodules to use the remote URL.
func (s *Syncer) reverseSyncLocalURLs(ctx context.Context) ([]URLChange, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	var changes []URLChange
	for _, path := range paths {
		if ctx.Err() != nil {
			return changes, ctx.Err()
		}

		declaredURL, err := git.GetDeclaredURL(ctx, s.repoRoot, path)
		if err != nil {
			continue
		}

		if !git.IsLocalFilesystemURL(declaredURL) {
			continue
		}

		subFullPath := filepath.Join(s.repoRoot, path)
		remoteURL, err := git.RemoteOriginURL(ctx, subFullPath)
		if err != nil || remoteURL == "" {
			continue
		}

		if git.IsLocalFilesystemURL(remoteURL) {
			continue
		}

		if err := git.SetDeclaredURL(ctx, s.repoRoot, path, remoteURL); err != nil {
			return changes, camperrors.Wrapf(err, "reverse-sync %s", path)
		}

		changes = append(changes, URLChange{
			Submodule: path,
			OldURL:    declaredURL,
			NewURL:    remoteURL,
		})
	}

	// Propagate changes to .git/config
	if len(changes) > 0 {
		cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
			"submodule", "sync", "--recursive")
		if output, syncErr := cmd.CombinedOutput(); syncErr != nil {
			return changes, camperrors.Wrapf(syncErr, "submodule sync after reverse-sync: %s",
				strings.TrimSpace(string(output)))
		}
	}

	return changes, nil
}
