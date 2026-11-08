//go:build container_fs

package sync

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/git"
)

// These legacy validation helpers are used only by the container suites.
// Production sync validates owner-aware metadata through validateInventory.

// verifySubmodules checks the status of all submodules after update.
func (s *Syncer) verifySubmodules(ctx context.Context) ([]SubmoduleResult, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}

	var results []SubmoduleResult
	for _, path := range paths {
		result := SubmoduleResult{
			Path:    path,
			Name:    path,
			Success: true,
		}

		fullPath := filepath.Join(s.repoRoot, path)
		initialized, identityErr := git.IsWorktreeRoot(ctx, fullPath)
		if identityErr != nil {
			result.Success = false
			result.Error = &SyncError{Op: "identify", Submodule: path, Cause: identityErr}
			results = append(results, result)
			continue
		}
		if !initialized {
			result.Success = false
			result.Error = &SyncError{Op: "identify", Submodule: path, Cause: git.ErrSubmoduleNotInitialized}
			results = append(results, result)
			continue
		}

		// Check if submodule directory exists and has content
		cmd := exec.CommandContext(ctx, "git", "-C", fullPath, "rev-parse", "HEAD")
		if _, err := cmd.Output(); err != nil {
			result.Success = false
			result.Error = git.ErrSubmoduleNotInitialized
		}

		// Check for detached HEAD
		cmd = exec.CommandContext(ctx, "git", "-C", fullPath, "symbolic-ref", "-q", "HEAD")
		if err := cmd.Run(); err != nil {
			result.HeadDetached = true
		}

		// Check working directory cleanliness
		hasChanges, _ := git.HasChanges(ctx, fullPath)
		result.WasClean = !hasChanges

		results = append(results, result)
	}

	return results, nil
}

// validateUpdate retains the package-level validation contract for callers
// that need one error. Sync reports each inventory error independently.
func (s *Syncer) validateUpdate(ctx context.Context) error {
	_, validationErrors := s.validateInventory(ctx)
	return errors.Join(validationErrors...)
}
