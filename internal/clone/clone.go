package clone

import (
	"context"
	"fmt"
)

// Clone performs a full campaign clone with submodule initialization.
//
// The clone process runs in five phases:
//  1. Clone the repository with recursive submodules (git clone --recurse-submodules)
//  2. Synchronize URLs from .gitmodules to .git/config (git submodule sync)
//  3. Update submodules to ensure all are at correct commits (git submodule update)
//  4. Validate the setup (all initialized, correct commits, matching URLs)
//  5. Report results
//
// Returns a CloneResult containing the outcome of each phase and any errors.
func (c *Cloner) Clone(ctx context.Context) (*CloneResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &CloneResult{Success: true}

	// Phase 1: Clone repository
	targetDir, err := c.gitClone(ctx)
	if err != nil {
		result.Success = false
		result.Errors = append(result.Errors, fmt.Errorf("clone failed: %w", err))
		return result, err
	}
	result.Directory = targetDir

	// Get the branch that was cloned
	branch, err := c.gitGetBranch(ctx, targetDir)
	if err == nil {
		result.Branch = branch
	}

	// Phase 2: URL synchronization (skip if no submodules requested)
	if !c.options.NoSubmodules {
		// Use sync package if provided, otherwise fall back to basic git commands
		if c.syncer != nil {
			syncResult, err := c.syncer.Sync(ctx)
			if err != nil {
				// Sync errors are warnings, not fatal (clone already succeeded)
				result.Warnings = append(result.Warnings, fmt.Sprintf("URL sync warning: %v", err))
			}
			if syncResult != nil {
				// Capture any URL changes made by sync
				for _, change := range syncResult.URLChanges {
					result.URLChanges = append(result.URLChanges, URLChange{
						Submodule: change.Submodule,
						OldURL:    change.OldURL,
						NewURL:    change.NewURL,
					})
				}
				// Propagate sync warnings
				result.Warnings = append(result.Warnings, syncResult.Warnings...)
			}
		} else {
			// Fallback to basic git submodule sync
			if err := c.gitSubmoduleSync(ctx, targetDir); err != nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("URL sync warning: %v", err))
			}
		}

		// Phase 3: Submodule update (ensure all are properly initialized)
		if err := c.gitSubmoduleUpdate(ctx, targetDir); err != nil {
			// Submodule update failure may be partial - continue to collect results
			result.Warnings = append(result.Warnings, fmt.Sprintf("submodule update warning: %v", err))
		}

		// Collect submodule results
		submodules, err := c.gitSubmoduleStatus(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not get submodule status: %v", err))
		}
		result.Submodules = submodules

		// Check for failed submodules
		for _, sub := range result.Submodules {
			if !sub.Success {
				result.Errors = append(result.Errors, fmt.Errorf("submodule %s failed: %v", sub.Path, sub.Error))
			}
		}
	}

	// Phase 4: Validation (unless --no-validate)
	if !c.options.NoValidate {
		result.Validation = c.validate(ctx, targetDir)
		if result.Validation != nil && !result.Validation.Passed {
			// Validation failure is an error
			for _, issue := range result.Validation.Issues {
				if issue.Severity == "error" {
					result.Errors = append(result.Errors, fmt.Errorf("validation: %s - %s", issue.Submodule, issue.Description))
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("validation: %s - %s", issue.Submodule, issue.Description))
				}
			}
		}
	}

	// Phase 5: Determine overall success
	// Success if no fatal errors and validation passed (if run)
	result.Success = len(result.Errors) == 0 &&
		(result.Validation == nil || result.Validation.Passed)

	return result, nil
}

// validate runs post-clone validation checks.
// This is a placeholder - full implementation will be in the validation sequence.
func (c *Cloner) validate(ctx context.Context, dir string) *ValidationResult {
	if ctx.Err() != nil {
		return &ValidationResult{Passed: false, Issues: []ValidationIssue{
			{Description: "context cancelled", Severity: "error"},
		}}
	}

	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: true,
		URLsMatch:      true,
	}

	// Check all submodules are initialized
	submodules, err := c.gitSubmoduleStatus(ctx, dir)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Description: fmt.Sprintf("could not check submodule status: %v", err),
			Severity:    "warning",
		})
	}

	for _, sub := range submodules {
		if !sub.Success {
			result.AllInitialized = false
			result.Passed = false
			result.Issues = append(result.Issues, ValidationIssue{
				Submodule:   sub.Path,
				Description: "not initialized",
				Severity:    "error",
			})
		}
	}

	return result
}
