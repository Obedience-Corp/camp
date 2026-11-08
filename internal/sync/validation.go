package sync

import (
	"bufio"
	"context"
	"fmt"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
)

// reportOrphanedGitlinks inventories undeclared gitlinks without mutating
// either the index or their working directories.
func (s *Syncer) reportOrphanedGitlinks(ctx context.Context) ([]git.OrphanedGitlink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	orphans, err := git.ListOrphanedGitlinks(ctx, s.repoRoot)
	if err != nil {
		return nil, err
	}

	return orphans, nil
}

// validateInventory checks each child against the metadata and gitlink in its
// immediate owning repository. An orphan mapping never hides later failures.
func (s *Syncer) validateInventory(ctx context.Context) ([]string, []error) {
	if ctx.Err() != nil {
		return nil, []error{ctx.Err()}
	}
	inventory := git.InventorySubmodules(ctx, s.repoRoot)
	var warnings []string
	var validationErrors []error
	for _, inventoryErr := range inventory.Errors {
		validationErrors = append(validationErrors, &SyncError{Op: "validate-metadata", Cause: inventoryErr})
	}
	for _, entry := range inventory.Entries {
		switch entry.State {
		case git.InventoryDeclarationOnly:
			validationErrors = append(validationErrors, &SyncError{Op: "validate-metadata", Submodule: entry.Path,
				Cause: camperrors.Newf("declared in owner %s .gitmodules without an index gitlink; reconcile metadata", entry.Owner)})
		case git.InventoryGitlinkOnly:
			warnings = append(warnings, fmt.Sprintf("orphaned gitlink without .gitmodules declaration: %s", entry.Path))
		case git.InventoryUninitialized:
			validationErrors = append(validationErrors, &SyncError{Op: "validate-init", Submodule: entry.Path,
				Cause: camperrors.Wrapf(git.ErrSubmoduleNotInitialized, "owner %s; run 'camp sync'", entry.Owner)})
		case git.InventoryMatched:
			matches, err := git.SelectedCommitMatches(ctx, s.repoRoot, entry)
			if err != nil {
				validationErrors = append(validationErrors, &SyncError{Op: "validate-ancestry", Submodule: entry.Path,
					Cause: camperrors.Wrapf(err, "owner %s", entry.Owner)})
			} else if !matches {
				validationErrors = append(validationErrors, &SyncError{Op: "validate-drift", Submodule: entry.Path,
					Cause: camperrors.Newf("checkout diverges from owner %s gitlink; inspect branch history before updating", entry.Owner)})
			}
		}
		if entry.State != git.InventoryMatched && entry.State != git.InventoryUninitialized {
			continue
		}
		matchesURL, active, err := git.InventoryURLMatches(ctx, s.repoRoot, entry)
		if err != nil {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path, Cause: err})
		} else if active == "" {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path,
				Cause: camperrors.Newf("active URL unavailable in owner %s", entry.Owner)})
		} else if !matchesURL {
			validationErrors = append(validationErrors, &SyncError{Op: "validate-url", Submodule: entry.Path,
				Cause: camperrors.Newf("active URL %q differs from declaration %q in owner %s; run 'camp sync'", active, entry.DeclaredURL, entry.Owner)})
		}
	}
	return warnings, validationErrors
}

func validateSubmoduleStatusOutput(output string) error {
	// Parse output for issues.
	// Format: [+- ]<sha1> <path> (<describe>)
	// '-' prefix = not initialized
	// '+' prefix = checked out commit differs from the recorded gitlink
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "-") {
			// Not initialized - extract path
			parts := strings.Fields(line)
			sub := line
			if len(parts) >= 2 {
				sub = parts[1]
			}
			return &SyncError{Op: "validate", Submodule: sub, Cause: git.ErrSubmoduleNotInitialized}
		}

		if strings.HasPrefix(line, "+") {
			parts := strings.Fields(line)
			sub := line
			recordedSHA := strings.TrimPrefix(line, "+")
			if len(parts) >= 2 {
				sub = parts[1]
				recordedSHA = strings.TrimPrefix(parts[0], "+")
			}
			return &SyncError{
				Op:        "validate-drift",
				Submodule: sub,
				Cause: camperrors.Wrapf(
					ErrSubmoduleValidation,
					"checked-out commit differs from recorded gitlink %s; run 'camp sync' or inspect the submodule",
					recordedSHA,
				),
			}
		}
	}

	return scanner.Err()
}
