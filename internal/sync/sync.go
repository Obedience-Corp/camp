package sync

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/remote"
)

// Sync performs safe submodule synchronization.
//
// The sync process runs in four phases:
//  1. Pre-flight checks: Detect uncommitted changes, unpushed commits, and URL mismatches
//  2. URL synchronization: Copy URLs from .gitmodules to .git/config
//  3. Submodule update: Initialize and update all submodules
//  4. Post-update validation: Verify owner metadata, selected commits, and active URLs
//
// In safe mode (default), sync aborts if pre-flight checks detect uncommitted
// changes or unpushed commits. Use WithForce(true) to skip safety checks.
//
// Returns a SyncResult containing the outcome of each phase and any errors.
func (s *Syncer) Sync(ctx context.Context) (*SyncResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.peer != nil && !s.options.VerifyArtifacts {
		if err := remote.RequirePeerSupport(); err != nil {
			return nil, err
		}
	}

	result := &SyncResult{Success: true}
	if links, linkErr := project.ListBrokenLinks(s.repoRoot); linkErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("optional project link inventory: %v", linkErr))
	} else {
		result.OptionalLinks = links
	}

	// Phase 1: Pre-flight checks (reuse cached result if available)
	var preflight *PreflightResult
	if s.cachedPreflight != nil {
		preflight = s.cachedPreflight
	} else {
		var err error
		preflight, err = s.RunPreflight(ctx)
		if err != nil {
			return nil, &SyncError{Op: "preflight", Cause: err}
		}
	}
	result.PreflightPassed = preflight.Passed
	result.Warnings = append(result.Warnings, s.collectWarnings(preflight)...)

	// In safe mode, abort if pre-flight failed
	if !preflight.Passed && !s.options.Force {
		result.Success = false
		return result, nil
	}

	// Dry-run stops here - just report what would happen
	if s.options.DryRun {
		// Add potential URL changes to result
		result.URLChanges = s.predictURLChanges(preflight)
		return result, nil
	}

	// Artifact-only variants keep the preflight above (every sync variant
	// runs it) but skip the git phases entirely.
	if s.options.VerifyArtifacts {
		s.verifyArtifacts(ctx, result)
		return result, nil
	}
	if s.options.ArtifactsOnly {
		s.pullArtifacts(ctx, result)
		return result, nil
	}

	// Phase 1.5: Report orphaned gitlinks without changing the index.
	orphans, orphanErr := s.reportOrphanedGitlinks(ctx)
	if orphanErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("orphan inventory warning: %v", orphanErr))
	}
	for _, orphan := range orphans {
		result.Warnings = append(result.Warnings, fmt.Sprintf("orphaned gitlink without .gitmodules declaration: %s", orphan.Path))
	}

	// Phase 1.7: Reverse-sync local filesystem URLs
	reverseChanges, reverseErr := s.reverseSyncLocalURLs(ctx)
	if reverseErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("reverse URL sync: %v", reverseErr))
	}
	result.URLChanges = append(result.URLChanges, reverseChanges...)

	// Phase 2: URL synchronization
	urlChanges, err := s.syncURLs(ctx)
	if err != nil {
		result.Success = false
		result.OperationFailed = true
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.URLChanges = append(result.URLChanges, urlChanges...)

	// Phase 3: Submodule update
	updateResults, err := s.updateSubmodules(ctx)
	if err != nil {
		result.Success = false
		result.OperationFailed = true
		result.Errors = append(result.Errors, err)
		return result, nil
	}
	result.UpdateResults = updateResults
	for _, sub := range updateResults {
		if !sub.Success {
			result.Success = false
			result.OperationFailed = true
			if sub.Error != nil {
				result.Errors = append(result.Errors, sub.Error)
			}
		}
		if sub.DriftWarning != "" {
			result.Success = false
			result.OperationFailed = true
			result.Warnings = append(result.Warnings, sub.DriftWarning)
		}
		if sub.PeerWarning != "" {
			result.Warnings = append(result.Warnings, sub.PeerWarning)
		}
	}
	if ctx.Err() != nil {
		result.Success = false
		result.OperationFailed = true
		if len(result.Errors) == 0 {
			result.Errors = append(result.Errors, ctx.Err())
		}
		return result, nil
	}

	// Phase 4: Post-update validation
	validationWarnings, validationErrors := s.validateInventory(ctx)
	result.ValidationRan = true
	for _, warning := range validationWarnings {
		found := false
		for _, existing := range result.Warnings {
			if warning == existing {
				found = true
				break
			}
		}
		if !found {
			result.Warnings = append(result.Warnings, warning)
		}
	}
	if len(validationErrors) > 0 {
		result.Success = false
		result.ValidationFailed = true
		result.Errors = append(result.Errors, validationErrors...)
	}
	if ctx.Err() != nil {
		result.OperationFailed = true
	}

	// Phase 5: Artifact pull rides the peer connection when one is
	// configured; policy=always roots only, and failures degrade to
	// warnings (the accelerated sync must not be worse than a plain one).
	if s.peer != nil && !s.options.GitOnly {
		beforeErrors := len(result.Errors)
		s.pullArtifacts(ctx, result)
		if len(result.Errors) > beforeErrors {
			result.OperationFailed = true
		}
	}

	return result, nil
}
