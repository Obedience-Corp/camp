package clone

import (
	"context"
	"os"
	"path/filepath"
)

// InitializedCheck verifies all submodules have content.
type InitializedCheck struct{}

// ID returns the unique identifier for this check.
func (c *InitializedCheck) ID() string { return "initialized" }

// Name returns the human-readable name.
func (c *InitializedCheck) Name() string { return "Submodule Initialization" }

// Run checks that all declared submodules have content in their directories.
func (c *InitializedCheck) Run(ctx context.Context, repoPath string) ([]ValidationIssue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var issues []ValidationIssue

	// Parse .gitmodules to find declared submodules
	submodules, err := parseGitmodules(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	for _, sub := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		subPath := filepath.Join(repoPath, sub.Path)

		// Check if directory exists and has content
		entries, err := os.ReadDir(subPath)
		if err != nil {
			issues = append(issues, ValidationIssue{
				CheckID:     c.ID(),
				Submodule:   sub.Path,
				Severity:    SeverityError,
				Description: "Submodule directory not found or inaccessible",
				FixCommand:  "git submodule update --init " + sub.Path,
				AutoFixable: true,
			})
			continue
		}

		if len(entries) == 0 {
			issues = append(issues, ValidationIssue{
				CheckID:     c.ID(),
				Submodule:   sub.Path,
				Severity:    SeverityError,
				Description: "Submodule directory is empty",
				FixCommand:  "git submodule update --init " + sub.Path,
				AutoFixable: true,
			})
		}
	}

	return issues, nil
}
