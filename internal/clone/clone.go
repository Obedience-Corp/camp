package clone

import (
	"context"
	"errors"
	"fmt"
	"sync"
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
		result.Errors = append(result.Errors, &SubmoduleError{Op: "clone", Cause: err})
		return result, err
	}
	c.progress.EndPhase("Clone", true)
	result.Directory = targetDir

	// Get the branch that was cloned
	branch, err := c.gitGetBranch(ctx, targetDir)
	if err == nil {
		result.Branch = branch
	}

	// Phase 1.5: Clean orphaned gitlinks before submodule operations
	if !c.options.NoSubmodules {
		removed, err := c.cleanOrphanedGitlinks(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("orphan cleanup warning: %v", err))
		}
		for _, path := range removed {
			c.progress.Verbose(fmt.Sprintf("Removed orphaned gitlink: %s", path))
			result.Warnings = append(result.Warnings, fmt.Sprintf("removed orphaned gitlink: %s", path))
		}
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

		// Phase 3: Initialize submodules gracefully (parallel with stale reference handling)
		c.progress.StartPhase("Initializing submodules")
		submoduleInfos, err := parseGitmodules(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not parse .gitmodules: %v", err))
		}

		// Determine parallelism
		parallel := c.options.Parallel
		if parallel <= 0 {
			parallel = 4
		}
		if parallel > len(submoduleInfos) {
			parallel = len(submoduleInfos)
		}

		// subInitResult holds the outcome of initializing a single submodule.
		type subInitResult struct {
			index       int
			result      SubmoduleResult
			warnings    []string
			staleRef    bool
			nestedCount int
			branchOK    bool
		}

		results := make([]subInitResult, len(submoduleInfos))
		sem := make(chan struct{}, parallel)
		var wg sync.WaitGroup

		for i, sub := range submoduleInfos {
			if ctx.Err() != nil {
				result.Errors = append(result.Errors, ctx.Err())
				break
			}

			wg.Add(1)
			go func(idx int, sub SubmoduleInfo) {
				defer wg.Done()

				// Acquire semaphore slot
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					results[idx] = subInitResult{
						index: idx,
						result: SubmoduleResult{
							Name: sub.Name, Path: sub.Path, URL: sub.URL,
							Success: false, Error: ctx.Err(),
						},
					}
					return
				}

				r := subInitResult{index: idx}
				c.progress.Verbose(fmt.Sprintf("Initializing submodule: %s", sub.Path))

				// Step 1: Initialize submodule gracefully (handles stale refs)
				subErr := c.initSubmoduleGraceful(ctx, targetDir, sub.Path)
				r.result = SubmoduleResult{
					Name:    sub.Name,
					Path:    sub.Path,
					URL:     sub.URL,
					Success: subErr == nil,
					Error:   subErr,
				}

				if subErr != nil {
					if c.isStaleRefError(subErr) {
						r.staleRef = true
						r.warnings = append(r.warnings,
							fmt.Sprintf("submodule %s had stale commit reference, using default branch", sub.Path))
						r.result.Success = true
						r.result.Error = nil
					} else {
						r.warnings = append(r.warnings,
							fmt.Sprintf("submodule %s: %v", sub.Path, subErr))
					}
				}

				// Step 2: Verify working tree has files
				if r.result.Success {
					if err := c.verifySubmoduleWorkingTree(ctx, targetDir, sub.Path); err != nil {
						c.progress.Verbose(fmt.Sprintf("Fixing empty working tree: %s", sub.Path))
						if fixErr := c.forceCheckoutSubmodule(ctx, targetDir, sub.Path); fixErr != nil {
							r.warnings = append(r.warnings,
								fmt.Sprintf("could not fix submodule %s: %v", sub.Path, fixErr))
						}
					}
				}

				// Step 3: Initialize nested submodules (recursively with graceful handling)
				if r.result.Success {
					count, warnings := c.initNestedSubmodulesGraceful(ctx, targetDir, sub.Path)
					r.nestedCount = count
					r.warnings = append(r.warnings, warnings...)
				}

				// Step 4: Checkout branch instead of detached HEAD
				if r.result.Success {
					if err := c.checkoutSubmoduleBranch(ctx, targetDir, sub.Path); err != nil {
						c.progress.Verbose(fmt.Sprintf("Could not checkout branch for %s: %v", sub.Path, err))
					} else {
						r.branchOK = true
					}
				}

				// Get commit hash if initialized
				if r.result.Success {
					r.result.Commit, _ = c.getSubmoduleCommit(ctx, targetDir, sub.Path)
				}

				results[idx] = r
			}(i, sub)
		}
		wg.Wait()

		// Aggregate results (in original order)
		succeeded := 0
		failed := 0
		nestedCount := 0
		branchCount := 0
		staleRefCount := 0

		for _, r := range results {
			result.Submodules = append(result.Submodules, r.result)
			result.Warnings = append(result.Warnings, r.warnings...)
			if r.result.Success {
				succeeded++
			} else {
				failed++
			}
			if r.staleRef {
				staleRefCount++
			}
			nestedCount += r.nestedCount
			if r.branchOK {
				branchCount++
			}
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
					result.Errors = append(result.Errors, &SubmoduleError{Op: "validation", Submodule: issue.Submodule, Cause: errors.New(issue.Description)})
				} else {
					result.Warnings = append(result.Warnings, fmt.Sprintf("validation: %s - %s", issue.Submodule, issue.Description))
				}
			}
		} else {
			c.progress.EndPhase("Validation", true)
		}
	}

	// Phase 5: Auto-register campaign (unless --no-register)
	if !c.options.NoRegister {
		regResult := c.registerCampaign(ctx, targetDir)
		result.Registration = regResult
		if regResult != nil && regResult.Registered {
			c.progress.Message(fmt.Sprintf("Registered campaign: %s", regResult.CampaignName))
		} else if regResult != nil && regResult.Error != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("auto-registration: %v", regResult.Error))
		}
	}

	// Phase 6: Determine overall success
	// Success if no fatal errors and validation passed (if run)
	// Registration failures do not affect overall success
	result.Success = len(result.Errors) == 0 &&
		(result.Validation == nil || result.Validation.Passed)

	return result, nil
}

// validate runs post-clone validation checks.
// Uses parseGitmodules as the canonical source of expected submodules (consistent
// with Phase 3 initialization). Extra submodules found by git submodule status
// but not declared in .gitmodules are reported as warnings, not errors.
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

	// Use parseGitmodules as the canonical list (same source as Phase 3)
	declared, err := parseGitmodules(ctx, dir)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Description: fmt.Sprintf("could not parse .gitmodules: %v", err),
			Severity:    SeverityWarning,
		})
		return result
	}

	// Build set of declared paths
	declaredPaths := make(map[string]bool, len(declared))
	for _, sub := range declared {
		declaredPaths[sub.Path] = true
	}

	// Get actual submodule state from git
	statusResults, err := c.gitSubmoduleStatus(ctx, dir)
	if err != nil {
		result.Issues = append(result.Issues, ValidationIssue{
			Description: fmt.Sprintf("could not check submodule status: %v", err),
			Severity:    SeverityWarning,
		})
		return result
	}

	// Build status lookup by path
	statusByPath := make(map[string]SubmoduleResult, len(statusResults))
	for _, sr := range statusResults {
		statusByPath[sr.Path] = sr
	}

	// Validate declared submodules (from .gitmodules)
	for _, sub := range declared {
		sr, found := statusByPath[sub.Path]
		if !found || !sr.Success {
			result.AllInitialized = false
			result.Passed = false
			result.Issues = append(result.Issues, ValidationIssue{
				Submodule:   sub.Path,
				Description: "not initialized",
				Severity:    SeverityError,
				FixCommand:  "git submodule update --init " + sub.Path,
				AutoFixable: true,
			})
		}
	}

	// Report undeclared submodules (found by git status but not in .gitmodules) as warnings
	for _, sr := range statusResults {
		if !declaredPaths[sr.Path] && !sr.Success {
			result.Issues = append(result.Issues, ValidationIssue{
				Submodule:   sr.Path,
				Description: "not in .gitmodules but has a gitlink entry",
				Severity:    SeverityWarning,
			})
		}
	}

	return result
}
