package checks

import (
	"context"
	"fmt"

	"github.com/obediencecorp/camp/internal/doctor"
	"github.com/obediencecorp/camp/internal/git"
)

// OrphanCheck detects orphaned gitlinks in the git index that have no
// corresponding entry in .gitmodules.
type OrphanCheck struct{}

// NewOrphanCheck creates a new orphaned gitlink check.
func NewOrphanCheck() *OrphanCheck {
	return &OrphanCheck{}
}

// ID returns the check identifier.
func (c *OrphanCheck) ID() string {
	return "orphan"
}

// Name returns the human-readable check name.
func (c *OrphanCheck) Name() string {
	return "Orphaned Gitlinks"
}

// Description returns a brief explanation of what this check does.
func (c *OrphanCheck) Description() string {
	return "Detects gitlink entries in the index with no matching .gitmodules declaration"
}

// Run performs the orphaned gitlink check.
func (c *OrphanCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Total:   0,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	orphans, err := git.ListOrphanedGitlinks(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("detect orphaned gitlinks: %w", err)
	}

	result.Total = len(orphans)

	for _, orphan := range orphans {
		result.Passed = false
		result.Issues = append(result.Issues, doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   orphan.Path,
			Description: fmt.Sprintf("Orphaned gitlink in index: %s (commit %s) has no .gitmodules entry", orphan.Path, orphan.Commit),
			FixCommand:  fmt.Sprintf("git rm --cached %s", orphan.Path),
			AutoFixable: true,
			Details: map[string]any{
				"commit": orphan.Commit,
				"type":   "orphaned_gitlink",
			},
		})
	}

	return result, nil
}

// Fix removes orphaned gitlinks from the git index.
func (c *OrphanCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(issues) == 0 {
		return nil, nil
	}

	// Rebuild the orphan list from issues
	var orphans []git.OrphanedGitlink
	for _, issue := range issues {
		if !issue.AutoFixable {
			continue
		}
		commit, _ := issue.Details["commit"].(string)
		orphans = append(orphans, git.OrphanedGitlink{
			Path:   issue.Submodule,
			Commit: commit,
		})
	}

	removed, err := git.RemoveOrphanedGitlinks(ctx, repoRoot, orphans)
	if err != nil {
		return nil, fmt.Errorf("remove orphaned gitlinks: %w", err)
	}

	// Map removed paths back to fixed issues
	removedSet := make(map[string]bool, len(removed))
	for _, p := range removed {
		removedSet[p] = true
	}

	var fixed []doctor.Issue
	for _, issue := range issues {
		if removedSet[issue.Submodule] {
			fixed = append(fixed, issue)
		}
	}

	return fixed, nil
}
