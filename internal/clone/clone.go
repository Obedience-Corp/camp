package clone

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	gitpkg "github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/remote"
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
	if c.peer != nil {
		if err := remote.RequirePeerSupport(); err != nil {
			return nil, err
		}
	}

	// Initialize progress reporter if not set
	if c.progress == nil {
		c.progress = &SilentReporter{}
	}

	result := &CloneResult{Success: true}

	// A peer that could not be reached is still worth reporting: every repo
	// came from origin, and the summary should say why rather than look like
	// no peer was asked for.
	if c.peerUnavailable != "" {
		result.Seed = append(result.Seed, SeedRepoResult{
			Repo: quiescenceRootRepo, Method: SeedMethodOrigin, Reason: c.peerUnavailable,
		})
	}

	// Phase 1: Clone repository (without --recurse-submodules to avoid all-or-nothing failure)
	c.progress.StartPhase("Cloning camp repository")
	var targetDir string
	var err error
	if c.peer != nil {
		targetDir, err = c.cloneRootFromPeer(ctx, result)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("peer clone from %s failed, falling back to origin: %v", c.peer.ID(), err))
			c.progress.Message(fmt.Sprintf("Peer clone from %s failed; cloning from origin", c.peer.ID()))
			targetDir, err = c.gitClone(ctx)
		}
	} else {
		targetDir, err = c.gitClone(ctx)
	}
	if err != nil {
		c.progress.EndPhase("Clone", false)
		result.Success = false
		result.Errors = append(result.Errors, &SubmoduleError{Op: "clone", Cause: err})
		return result, err
	}
	c.progress.EndPhase("Clone", true)
	result.Directory = targetDir
	if links, linkErr := project.ListBrokenLinks(targetDir); linkErr != nil {
		result.Warnings = append(result.Warnings, fmt.Sprintf("optional project link inventory: %v", linkErr))
	} else {
		result.OptionalLinks = links
	}

	// Get the branch that was cloned
	branch, err := c.gitGetBranch(ctx, targetDir)
	if err == nil {
		result.Branch = branch
	}

	// Phase 1.5: Inventory orphaned gitlinks without changing the clone's index.
	if !c.options.NoSubmodules {
		orphans, err := c.reportOrphanedGitlinks(ctx, targetDir)
		if err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("orphan inventory warning: %v", err))
		}
		for _, orphan := range orphans {
			result.Warnings = append(result.Warnings, fmt.Sprintf("orphaned gitlink without .gitmodules declaration: %s", orphan.Path))
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
			result.Errors = append(result.Errors, &SubmoduleError{Op: "parse-gitmodules", Cause: err})
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
			nestedCount int
			branchOK    bool
			// seed records which transport delivered this submodule. Written
			// per goroutine and aggregated after the wait, like every other
			// field here, so the parallel loop needs no lock.
			seed SeedRepoResult
		}

		results := make([]subInitResult, len(submoduleInfos))
		sem := make(chan struct{}, parallel)
		var wg sync.WaitGroup

		for i, sub := range submoduleInfos {
			if ctx.Err() != nil {
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
							Success: false, Skipped: true, Operation: "skip", Error: ctx.Err(),
						},
					}
					return
				}
				// Peer seeding, registration, update, checkout, and nested discovery
				// may all touch the same superproject Git state. Serialize the full
				// pipeline for this owner, including the stale-object fallback.
				release, lockErr := gitpkg.LockSubmoduleOwner(ctx, targetDir)
				if lockErr != nil {
					skipped := errors.Is(lockErr, context.Canceled) || errors.Is(lockErr, context.DeadlineExceeded)
					op := "init"
					if skipped {
						op = "skip"
					}
					results[idx] = subInitResult{index: idx, result: SubmoduleResult{
						Name: sub.Name, Path: sub.Path, URL: sub.URL,
						Success: false, Skipped: skipped, Operation: op, Error: camperrors.Wrapf(lockErr, "%s %s in %s", op, sub.Path, targetDir),
					}}
					return
				}
				defer release()

				r := subInitResult{index: idx}
				c.progress.Verbose(fmt.Sprintf("Initializing submodule: %s", sub.Path))

				// Step 0: Seed objects from the peer when one is configured.
				// Cold-seed copy first when the peer declared this repo
				// quiescent, then the git-clone-from-peer path, then origin —
				// each step degrades to the next with a warning rather than
				// failing the submodule.
				peerSeeded := false
				if c.peer != nil {
					method, reason, coldErr := c.coldSeedSubmodule(ctx, targetDir, sub)
					if coldErr == nil {
						peerSeeded = true
						r.seed = SeedRepoResult{Repo: sub.Path, Method: method, Reason: reason}
					} else {
						if !errors.Is(coldErr, errColdSeedSkipped) {
							r.warnings = append(r.warnings,
								fmt.Sprintf("cold-seed %s: %v (cloning from peer)", sub.Path, coldErr))
							reason = coldErr.Error()
						}
						if seedErr := c.seedSubmoduleFromPeer(ctx, targetDir, sub); seedErr != nil {
							r.warnings = append(r.warnings,
								fmt.Sprintf("peer seed %s: %v (initializing from origin)", sub.Path, seedErr))
							r.seed = SeedRepoResult{Repo: sub.Path, Method: SeedMethodOrigin, Reason: seedErr.Error()}
						} else {
							peerSeeded = true
							r.seed = SeedRepoResult{Repo: sub.Path, Method: SeedMethodPeerClone, Reason: reason}
						}
					}
				}

				// Step 1: Initialize submodule gracefully (handles stale refs)
				initOutcome, subErr := c.initSubmoduleGraceful(ctx, targetDir, sub.Path)
				if subErr == nil {
					initialized, identityErr := gitpkg.IsWorktreeRoot(ctx, filepath.Join(targetDir, sub.Path))
					if identityErr != nil {
						subErr = camperrors.Wrapf(identityErr, "identify initialized submodule %s", sub.Path)
					} else if !initialized {
						subErr = camperrors.Wrapf(gitpkg.ErrSubmoduleNotInitialized, "identify initialized submodule %s", sub.Path)
					}
				}
				r.result = SubmoduleResult{
					Name:             sub.Name,
					Path:             sub.Path,
					URL:              sub.URL,
					Success:          subErr == nil,
					Initialized:      subErr == nil,
					Operation:        "init",
					Error:            subErr,
					PeerSeeded:       peerSeeded,
					StaleRefFallback: initOutcome.StaleRefFallback,
				}

				if subErr != nil {
					r.warnings = append(r.warnings,
						fmt.Sprintf("submodule %s: %v", sub.Path, subErr))
				}
				if r.result.Success && (r.seed.Method == SeedMethodPackCopy || r.seed.Method == SeedMethodBundle) {
					// Cold seeds carry objects but may lack the real origin's
					// tracking refs. Refresh before mandatory branch selection.
					cmd := exec.CommandContext(ctx, "git", "-C", targetDir, "submodule", "sync", "--", sub.Path)
					if output, syncErr := cmd.CombinedOutput(); syncErr != nil {
						subErr = camperrors.Wrapf(syncErr, "sync seeded %s URL: %s", sub.Path, strings.TrimSpace(string(output)))
					} else {
						subErr = gitpkg.RefreshSubmoduleOrigin(ctx, filepath.Join(targetDir, sub.Path))
					}
					if subErr != nil {
						r.result.Success = false
						r.result.Operation = "fetch"
						r.result.Error = subErr
						r.warnings = append(r.warnings, fmt.Sprintf("submodule %s origin refresh: %v", sub.Path, subErr))
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

				// Step 3: Select the final parent checkout before discovering its children.
				if r.result.Success {
					if err := c.checkoutSubmoduleBranch(ctx, targetDir, sub.Path, peerSeeded); err != nil {
						r.result.Success = false
						r.result.Operation = "checkout"
						r.result.Error = err
						r.warnings = append(r.warnings, fmt.Sprintf("submodule %s checkout: %v", sub.Path, err))
					} else {
						r.branchOK = true
					}
				}
				if r.result.Success {
					r.result.Commit, _ = c.getSubmoduleCommit(ctx, targetDir, sub.Path)
					if pin, err := gitpkg.Output(ctx, targetDir, "ls-files", "-s", "--", sub.Path); err == nil {
						fields := strings.Fields(pin)
						if len(fields) >= 2 && fields[0] == "160000" {
							r.result.RecordedCommit = fields[1]
						}
					}
				}

				// Step 4: Discover children from the selected branch, not the
				// earlier gitlink snapshot.
				if r.result.Success {
					r.result.Nested = c.initNestedSubmodulesGraceful(ctx, targetDir, sub.Path)
					r.nestedCount = gitpkg.SummarizeNestedResults(r.result.Nested).Initialized
					if nestedErr := gitpkg.NestedFailures(r.result.Nested); nestedErr != nil {
						r.result.Success = false
						r.result.Operation = "children"
						r.result.Error = nestedErr
					}
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
		peerSeededCount := 0

		for i, r := range results {
			if r.result.Path == "" {
				// Cancellation can stop scheduling before the last goroutine was
				// launched. Keep those paths visible in ordered results.
				sub := submoduleInfos[i]
				r.result = SubmoduleResult{Name: sub.Name, Path: sub.Path, URL: sub.URL,
					Skipped: true, Operation: "skip", Error: ctx.Err()}
			}
			result.Submodules = append(result.Submodules, r.result)
			result.Warnings = append(result.Warnings, r.warnings...)
			if r.result.Success {
				succeeded++
			} else {
				failed++
				if r.result.Error != nil {
					result.Errors = append(result.Errors, r.result.Error)
				}
			}
			nestedCount += r.nestedCount
			if r.branchOK {
				branchCount++
			}
			if r.result.PeerSeeded {
				peerSeededCount++
			}
			if r.seed.Repo != "" {
				result.Seed = append(result.Seed, r.seed)
			}
		}

		c.progress.EndSubmodules(succeeded, failed)

		if peerSeededCount > 0 && c.peer != nil {
			c.progress.Message(fmt.Sprintf("Seeded %d submodules from peer %s", peerSeededCount, c.peer.ID()))
		}
		if nestedCount > 0 {
			c.progress.Message(fmt.Sprintf("Initialized %d nested submodules", nestedCount))
		}
		if branchCount > 0 {
			c.progress.Message(fmt.Sprintf("Checked out branches for %d submodules", branchCount))
		}
		c.progress.EndPhase("Submodule initialization", failed == 0)
	}
	if ctx.Err() != nil {
		if len(result.Errors) == 0 {
			result.Errors = append(result.Errors, ctx.Err())
		}
		result.Success = false
		return result, nil
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
			c.progress.Message(fmt.Sprintf("Registered camp: %s", regResult.CampaignName))
		} else if regResult != nil && regResult.Error != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("auto-registration: %v", regResult.Error))
		}
	}

	// Phase 6: Required operations determine success even when optional
	// validation was skipped. Registration and local link setup are optional.
	operationsPassed := true
	for _, sub := range result.Submodules {
		if !sub.Success || !nestedCheckoutSucceeded(sub.Nested) {
			operationsPassed = false
		}
	}
	result.Success = operationsPassed && len(result.Errors) == 0 &&
		(result.Validation == nil || result.Validation.Passed)

	return result, nil
}

func nestedCheckoutSucceeded(results []gitpkg.NestedResult) bool {
	for _, result := range results {
		if !result.Success || !nestedCheckoutSucceeded(result.Children) {
			return false
		}
	}
	return true
}

// validate runs post-clone validation checks.
// Compares declarations and gitlinks within each repository that owns them.
func (c *Cloner) validate(ctx context.Context, dir string) *ValidationResult {
	if ctx.Err() != nil {
		return &ValidationResult{Passed: false,
			CheckStates: map[string]string{"allInitialized": "unavailable", "correctCommits": "unavailable", "urlsMatch": "unavailable"},
			Issues: []ValidationIssue{
				{Description: "context cancelled", Severity: SeverityError},
			}}
	}

	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: true,
		URLsMatch:      true,
		CheckStates:    map[string]string{"allInitialized": "passed", "correctCommits": "passed", "urlsMatch": "passed"},
	}
	setState := func(check, state string) {
		current := result.CheckStates[check]
		if current == "failed" || (current == "unavailable" && state != "failed") {
			return
		}
		result.CheckStates[check] = state
	}

	// --no-submodules leaves declared entries uninitialized on purpose.
	if c.options.NoSubmodules {
		result.AllInitialized = false
		result.CorrectCommits = false
		result.URLsMatch = false
		for check := range result.CheckStates {
			result.CheckStates[check] = "skipped"
		}
		result.CheckResults = map[string]bool{"allInitialized": false, "correctCommits": false, "urlsMatch": false}
		return result
	}

	inventory := gitpkg.InventorySubmodules(ctx, dir)
	for _, inventoryErr := range inventory.Errors {
		result.Passed = false
		result.AllInitialized = false
		result.CorrectCommits = false
		result.URLsMatch = false
		setState("allInitialized", "unavailable")
		setState("correctCommits", "unavailable")
		setState("urlsMatch", "unavailable")
		result.Issues = append(result.Issues, ValidationIssue{
			CheckID: "metadata", Description: inventoryErr.Error(), Severity: SeverityError,
		})
	}
	for _, entry := range inventory.Entries {
		switch entry.State {
		case gitpkg.InventoryDeclarationOnly:
			result.Passed = false
			result.AllInitialized = false
			result.CorrectCommits = false
			result.URLsMatch = false
			setState("allInitialized", "failed")
			setState("correctCommits", "unavailable")
			setState("urlsMatch", "unavailable")
			result.Issues = append(result.Issues, ValidationIssue{
				CheckID: "declaration_without_gitlink", Submodule: entry.Path,
				Description: "declared in .gitmodules but has no gitlink; reconcile the declaration and index",
				Severity:    SeverityError,
			})
		case gitpkg.InventoryGitlinkOnly:
			result.Issues = append(result.Issues, ValidationIssue{
				CheckID: "gitlink_without_declaration", Submodule: entry.Path,
				Description: "gitlink has no .gitmodules declaration in its owning repository",
				Severity:    SeverityWarning,
			})
		case gitpkg.InventoryUninitialized:
			result.Passed = false
			result.AllInitialized = false
			result.CorrectCommits = false
			setState("allInitialized", "failed")
			setState("correctCommits", "unavailable")
			result.Issues = append(result.Issues, ValidationIssue{
				CheckID: "not_initialized", Submodule: entry.Path,
				Description: "not initialized", Severity: SeverityError,
				FixCommand: "camp sync", AutoFixable: true,
			})
		case gitpkg.InventoryInvalidPath, gitpkg.InventoryUnknownMapping:
			// The inventory already records the metadata or path error.
			continue
		}
		if entry.State != gitpkg.InventoryMatched && entry.State != gitpkg.InventoryUninitialized {
			continue
		}
		if entry.State == gitpkg.InventoryMatched {
			matches, err := gitpkg.SelectedCommitMatches(ctx, dir, entry)
			switch {
			case err != nil:
				result.CorrectCommits = false
				result.Passed = false
				setState("correctCommits", "unavailable")
				result.Issues = append(result.Issues, ValidationIssue{CheckID: "correct_commits", Submodule: entry.Path,
					Description: camperrors.Wrapf(err, "compare checkout with gitlink in owner %s", entry.Owner).Error(), Severity: SeverityError})
			case !matches:
				result.CorrectCommits = false
				result.Passed = false
				setState("correctCommits", "failed")
				result.Issues = append(result.Issues, ValidationIssue{CheckID: "correct_commits", Submodule: entry.Path,
					Description: "checkout diverges from owning repository's recorded gitlink; inspect branch history before updating", Severity: SeverityError})
			}
		}
		matchesURL, active, urlErr := gitpkg.InventoryURLMatches(ctx, dir, entry)
		switch {
		case urlErr != nil || active == "":
			result.URLsMatch = false
			result.Passed = false
			setState("urlsMatch", "unavailable")
			description := "active URL is unavailable in owning repository"
			if urlErr != nil {
				description = urlErr.Error()
			}
			result.Issues = append(result.Issues, ValidationIssue{CheckID: "urls_match", Submodule: entry.Path,
				Description: description, Severity: SeverityError})
		case !matchesURL:
			result.URLsMatch = false
			result.Passed = false
			setState("urlsMatch", "failed")
			result.Issues = append(result.Issues, ValidationIssue{CheckID: "urls_match", Submodule: entry.Path,
				Description: fmt.Sprintf("active URL %q differs from declaration %q in owner %s; run 'camp sync' to synchronize", active, entry.DeclaredURL, entry.Owner),
				Severity:    SeverityError, FixCommand: "camp sync", AutoFixable: true})
		}
	}
	result.CheckResults = map[string]bool{
		"allInitialized": result.AllInitialized,
		"correctCommits": result.CorrectCommits,
		"urlsMatch":      result.URLsMatch,
	}

	return result
}
