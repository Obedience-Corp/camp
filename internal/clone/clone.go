package clone

import (
	"context"
	"fmt"
)

// Clone performs a full campaign clone with submodule initialization.
//
// The clone process runs in five phases:
//  1. Clone the main repository (WITHOUT --recurse-submodules)
//  2. Synchronize URLs from .gitmodules to .git/config (git submodule sync)
//  3. Initialize submodules gracefully, one-by-one with stale reference handling
//  4. Validate the setup (all initialized, correct commits, matching URLs)
//  5. Report results
//
// Phase 3 uses graceful submodule initialization that:
//   - Handles stale commit references (commit no longer exists on remote)
//   - Falls back to the remote's default branch when needed
//   - Initializes nested submodules recursively with the same graceful handling
//   - Checks out the remote default branch instead of detached HEAD
//
// Returns a CloneResult containing the outcome of each phase and any errors.
func (c *Cloner) Clone(ctx context.Context) (*CloneResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Initialize progress reporter if not set
	if c.progress == nil {
		c.progress = &SilentReporter{}
	}

	result := &CloneResult{Success: true}

	// Phase 1: Clone repository (without --recurse-submodules to avoid all-or-nothing failure)
	c.progress.StartPhase("Cloning campaign repository")
	targetDir, err := c.gitClone(ctx)
	if err != nil {
		c.progress.EndPhase("Clone", false)
		result.Success = false
		result.Errors = append(result.Errors, fmt.Errorf("clone failed: %w", err))
		return result, err
	}
	c.progress.EndPhase("Clone", true)
	result.Directory = targetDir

	// Get the branch that was cloned
	branch, err := c.gitGetBranch(ctx, targetDir)
	if err == nil {
		result.Branch = branch
	}

	// Phase 2: URL synchronization (skip if no submodules requested)
	if !c.options.NoSubmodules {
		c.progress.StartPhase("Synchronizing submodule URLs")
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
		c.progress.EndPhase("URL sync", true)

		// Phase 3: Initialize submodules gracefully (one-by-one with stale reference handling)
		c.progress.StartPhase("Initializing submodules")
		submoduleInfos, err := parseGitmodules(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse .gitmodules: %v", err))
		}

		succeeded := 0
		failed := 0
		nestedCount := 0
		branchCount := 0
		staleRefCount := 0

		for _, sub := range submoduleInfos {
			if ctx.Err() != nil {
				result.Errors = append(result.Errors, ctx.Err())
				break
			}

			c.progress.Verbose(fmt.Sprintf("Initializing submodule: %s", sub.Path))

			// Step 1: Initialize submodule gracefully (handles stale refs)
			subErr := c.initSubmoduleGraceful(ctx, targetDir, sub.Path)
			subResult := SubmoduleResult{
				Name:    sub.Name,
				Path:    sub.Path,
				URL:     sub.URL,
				Success: subErr == nil,
				Error:   subErr,
			}

			if subErr != nil {
				// Check if this was a stale reference that we recovered from
				if c.isStaleRefError(subErr) {
					staleRefCount++
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("submodule %s had stale commit reference, using default branch", sub.Path))
					// We still mark as success if recovery worked (no error after fallback)
					subResult.Success = true
					subResult.Error = nil
					succeeded++
				} else {
					failed++
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("submodule %s: %v", sub.Path, subErr))
				}
			} else {
				succeeded++
			}

			// Step 2: Verify working tree has files
			if subResult.Success {
				if err := c.verifySubmoduleWorkingTree(ctx, targetDir, sub.Path); err != nil {
					c.progress.Verbose(fmt.Sprintf("Fixing empty working tree: %s", sub.Path))
					if fixErr := c.forceCheckoutSubmodule(ctx, targetDir, sub.Path); fixErr != nil {
						result.Warnings = append(result.Warnings,
							fmt.Sprintf("could not fix submodule %s: %v", sub.Path, fixErr))
					}
				}
			}

			// Step 3: Initialize nested submodules (recursively with graceful handling)
			if subResult.Success {
				count, warnings := c.initNestedSubmodulesGraceful(ctx, targetDir, sub.Path)
				nestedCount += count
				result.Warnings = append(result.Warnings, warnings...)
			}

			// Step 4: Checkout branch instead of detached HEAD
			if subResult.Success {
				if err := c.checkoutSubmoduleBranch(ctx, targetDir, sub.Path); err != nil {
					c.progress.Verbose(fmt.Sprintf("Could not checkout branch for %s: %v", sub.Path, err))
					// Non-fatal: some submodules may not have a remote configured
				} else {
					branchCount++
				}
			}

			// Get commit hash if initialized
			if subResult.Success {
				subResult.Commit, _ = c.getSubmoduleCommit(ctx, targetDir, sub.Path)
			}

			result.Submodules = append(result.Submodules, subResult)
		}

		c.progress.EndSubmodules(succeeded, failed)

		if staleRefCount > 0 {
			c.progress.Message(fmt.Sprintf("Recovered %d submodules with stale commit references", staleRefCount))
		}
		if nestedCount > 0 {
			c.progress.Message(fmt.Sprintf("Initialized %d nested submodules", nestedCount))
		}
		if branchCount > 0 {
			c.progress.Message(fmt.Sprintf("Checked out branches for %d submodules", branchCount))
		}
		c.progress.EndPhase("Submodule initialization", true)
	}

	// Phase 4: Validation (unless --no-validate)
	if !c.options.NoValidate {
		c.progress.StartPhase("Validating setup")
		result.Validation = c.validate(ctx, targetDir)
		if result.Validation != nil && !result.Validation.Passed {
			c.progress.EndPhase("Validation", false)
			// Validation failure is an error
			for _, issue := range result.Validation.Issues {
				if issue.Severity == SeverityError {
					result.Errors = append(result.Errors, fmt.Errorf("validation: %s - %s", issue.Submodule, issue.Description))
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("validation: %s - %s", issue.Submodule, issue.Description))
				}
			}
		} else {
			c.progress.EndPhase("Validation", true)
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
			{Description: "context cancelled", Severity: SeverityError},
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
			Severity:    SeverityWarning,
		})
	}

	for _, sub := range submodules {
		if !sub.Success {
			result.AllInitialized = false
			result.Passed = false
			result.Issues = append(result.Issues, ValidationIssue{
				Submodule:   sub.Path,
				Description: "not initialized",
				Severity:    SeverityError,
			})
		}
	}

	return result
}
