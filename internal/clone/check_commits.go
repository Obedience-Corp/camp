package clone

import (
	"context"
	"os/exec"
	"strings"
)

// CommitCheck verifies submodules are at the correct commits.
type CommitCheck struct{}

// ID returns the unique identifier for this check.
func (c *CommitCheck) ID() string { return "commits" }

// Name returns the human-readable name.
func (c *CommitCheck) Name() string { return "Commit References" }

// Run checks that all submodules are at the commit recorded in the parent repository.
func (c *CommitCheck) Run(ctx context.Context, repoPath string) ([]ValidationIssue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var issues []ValidationIssue

	// Build set of declared submodule paths from .gitmodules for ghost detection
	declared := make(map[string]bool)
	submodules, _ := parseGitmodules(ctx, repoPath)
	for _, sub := range submodules {
		declared[sub.Path] = true
	}

	// Run git submodule status
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "submodule", "status", "--recursive")
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Parse status output
	// Format: " <commit> <path> (<branch>)" or "-<commit> <path>" (not initialized) or "+<commit> <path>" (wrong commit)
	lines := strings.Split(string(output), "\n")
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}

		prefix := line[0]
		switch prefix {
		case '-':
			// Not initialized — check if this is a ghost gitlink (not in .gitmodules)
			parts := strings.Fields(line[1:])
			if len(parts) >= 2 {
				path := parts[1]
				if declared[path] {
					issues = append(issues, ValidationIssue{
						CheckID:     c.ID(),
						Submodule:   path,
						Severity:    SeverityError,
						Description: "Submodule not initialized",
						FixCommand:  "git submodule update --init " + path,
						AutoFixable: true,
					})
				} else {
					issues = append(issues, ValidationIssue{
						CheckID:     c.ID(),
						Submodule:   path,
						Severity:    SeverityWarning,
						Description: "Stale gitlink with no .gitmodules entry (ghost submodule)",
						FixCommand:  "git rm --cached " + path,
						AutoFixable: true,
					})
				}
			}
		case '+':
			// Wrong commit - submodule HEAD differs from parent's recorded commit
			parts := strings.Fields(line[1:])
			if len(parts) >= 2 {
				issues = append(issues, ValidationIssue{
					CheckID:     c.ID(),
					Submodule:   parts[1],
					Severity:    SeverityWarning,
					Description: "Submodule at different commit than parent expects",
					FixCommand:  "git submodule update " + parts[1],
					AutoFixable: true,
				})
			}
		}
	}

	return issues, nil
}
