package clone

import (
	"context"
	"fmt"
)

// Clone performs a full campaign clone with submodule initialization.
//
// The clone process runs in six phases:
//  1. Clone the repository with recursive submodules (git clone --recurse-submodules)
//  2. Synchronize URLs from .gitmodules to .git/config (git submodule sync)
//  3. Update submodules to ensure all are at correct commits (git submodule update)
//  3.5. Verify working trees, init nested submodules, checkout branches
//  4. Validate the setup (all initialized, correct commits, matching URLs)
//  5. Report results
//
// Phase 3.5 addresses common clone issues:
//   - Empty working trees (git metadata exists but files not checked out)
//   - Uninitialized nested submodules (submodules within submodules)
//   - Detached HEAD state (checks out remote default branch instead)
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

	// Phase 1: Clone repository
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

		// Phase 3: Submodule update (ensure all are properly initialized)
		c.progress.StartPhase("Updating submodules")
		if err := c.gitSubmoduleUpdate(ctx, targetDir); err != nil {
			// Submodule update failure may be partial - continue to collect results
			result.Warnings = append(result.Warnings, fmt.Sprintf("submodule update warning: %v", err))
		}
		c.progress.EndPhase("Submodule update", true)

		// Collect submodule results
		submodules, err := c.gitSubmoduleStatus(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not get submodule status: %v", err))
		}
		result.Submodules = submodules

		// Report submodule progress
		if len(result.Submodules) > 0 {
			succeeded := 0
			failed := 0
			for _, sub := range result.Submodules {
				if sub.Success {
					succeeded++
				} else {
					failed++
					result.Errors = append(result.Errors, fmt.Errorf("submodule %s failed: %v", sub.Path, sub.Error))
				}
			}
			c.progress.EndSubmodules(succeeded, failed)
		}

		// Phase 3.5: Verify working trees, init nested submodules, checkout branches
		c.progress.StartPhase("Verifying submodule working trees")
		submoduleInfos, err := parseGitmodules(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse .gitmodules: %v", err))
		}

		fixedCount := 0
		nestedCount := 0
		branchCount := 0
		for _, sub := range submoduleInfos {
			// Step 1: Verify and fix empty working trees
			if err := c.verifySubmoduleWorkingTree(ctx, targetDir, sub.Path); err != nil {
				c.progress.Verbose(fmt.Sprintf("Fixing empty working tree: %s", sub.Path))
				if fixErr := c.forceCheckoutSubmodule(ctx, targetDir, sub.Path); fixErr != nil {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("could not fix submodule %s: %v", sub.Path, fixErr))
					continue // Skip remaining steps if checkout failed
				}
				fixedCount++
			}

			// Step 2: Initialize nested submodules
			if err := c.initNestedSubmodules(ctx, targetDir, sub.Path); err != nil {
				result.Warnings = append(result.Warnings,
					fmt.Sprintf("could not init nested submodules in %s: %v", sub.Path, err))
			} else {
				// Count nested submodules if any were initialized
				nestedSubs, _ := parseGitmodules(ctx, fmt.Sprintf("%s/%s", targetDir, sub.Path))
				nestedCount += len(nestedSubs)
			}

			// Step 3: Checkout branch instead of detached HEAD
			if err := c.checkoutSubmoduleBranch(ctx, targetDir, sub.Path); err != nil {
				c.progress.Verbose(fmt.Sprintf("Could not checkout branch for %s: %v", sub.Path, err))
				// Non-fatal: some submodules may not have a remote configured
			} else {
				branchCount++
			}
		}

		if fixedCount > 0 {
			c.progress.Message(fmt.Sprintf("Fixed %d submodules with empty working trees", fixedCount))
		}
		if nestedCount > 0 {
			c.progress.Message(fmt.Sprintf("Initialized %d nested submodules", nestedCount))
		}
		if branchCount > 0 {
			c.progress.Message(fmt.Sprintf("Checked out branches for %d submodules", branchCount))
		}
		c.progress.EndPhase("Working tree verification", true)
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
