package checks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/doctor"
)

// CommitsCheck verifies parent-submodule commit synchronization.
type CommitsCheck struct{}

// NewCommitsCheck creates a new commit reference check.
func NewCommitsCheck() *CommitsCheck {
	return &CommitsCheck{}
}

// ID returns the check identifier.
func (c *CommitsCheck) ID() string {
	return "commits"
}

// Name returns the human-readable check name.
func (c *CommitsCheck) Name() string {
	return "Commit Reference"
}

// Description returns a brief explanation of what this check does.
func (c *CommitsCheck) Description() string {
	return "Verifies parent-submodule commit synchronization"
}

// Run performs the commit reference check.
func (c *CommitsCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Total:   0,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	// Get submodule paths
	submodules, err := c.listSubmodules(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("list submodules: %w", err)
	}

	result.Total = len(submodules)
	alignedCount := 0

	// Check each submodule
	for _, path := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(repoRoot, path)

		// Get expected commit from parent
		expected, err := c.getExpectedCommit(ctx, repoRoot, path)
		if err != nil {
			// Skip if we can't get expected commit
			continue
		}

		// Get actual commit from submodule
		actual, err := c.getActualCommit(ctx, fullPath)
		if err != nil {
			// Skip if submodule is not initialized
			continue
		}

		if expected == actual {
			// Commits match - all good
			alignedCount++
			continue
		}

		// Commits don't match - determine the issue type
		issue := c.analyzeCommitMismatch(ctx, path, fullPath, expected, actual)
		result.Passed = false
		result.Issues = append(result.Issues, issue)
	}

	result.Details["aligned"] = alignedCount
	result.Details["misaligned"] = len(result.Issues)

	return result, nil
}

// Fix attempts to repair commit mismatches by updating submodules.
func (c *CommitsCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(issues) == 0 {
		return nil, nil
	}

	var fixed []doctor.Issue
	for _, issue := range issues {
		if !issue.AutoFixable {
			continue
		}

		// Run git submodule update for this submodule
		cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "submodule", "update", issue.Submodule)
		if err := cmd.Run(); err != nil {
			continue // Skip if fix fails
		}

		fixed = append(fixed, issue)
	}

	return fixed, nil
}

// listSubmodules returns all submodule paths from .gitmodules.
func (c *CommitsCheck) listSubmodules(ctx context.Context, repoRoot string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot,
		"config", "-f", ".gitmodules", "--get-regexp", "^submodule\\..*\\.path$")

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// No submodules configured
			return nil, nil
		}
		return nil, fmt.Errorf("list submodules: %w", err)
	}

	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			paths = append(paths, parts[1])
		}
	}

	return paths, nil
}

// getExpectedCommit gets the commit the parent expects for a submodule.
func (c *CommitsCheck) getExpectedCommit(ctx context.Context, repoRoot, subPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-tree", "HEAD", subPath)
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ls-tree: %w", err)
	}

	// Format: "mode type hash\tpath"
	// Example: "160000 commit a1b2c3d4e5f6...\tprojects/camp"
	fields := strings.Fields(strings.TrimSpace(string(output)))
	if len(fields) < 3 {
		return "", fmt.Errorf("unexpected ls-tree output: %q", string(output))
	}

	return fields[2], nil
}

// getActualCommit gets the current HEAD commit of a submodule.
func (c *CommitsCheck) getActualCommit(ctx context.Context, subPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", subPath, "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("rev-parse: %w", err)
	}

	return strings.TrimSpace(string(output)), nil
}

// commitExists checks if a commit exists in the submodule repository.
func (c *CommitsCheck) commitExists(ctx context.Context, subPath, commit string) bool {
	cmd := exec.CommandContext(ctx, "git", "-C", subPath, "cat-file", "-e", commit)
	err := cmd.Run()
	return err == nil
}

// isCommitAhead checks if commit1 is ahead of commit2 in the history.
func (c *CommitsCheck) isCommitAhead(ctx context.Context, subPath, commit1, commit2 string) bool {
	// Check if commit2 is an ancestor of commit1
	cmd := exec.CommandContext(ctx, "git", "-C", subPath, "merge-base", "--is-ancestor", commit2, commit1)
	err := cmd.Run()
	return err == nil
}

// analyzeCommitMismatch determines the type of commit mismatch.
func (c *CommitsCheck) analyzeCommitMismatch(ctx context.Context, path, fullPath, expected, actual string) doctor.Issue {
	// Check if expected commit exists in submodule
	if !c.commitExists(ctx, fullPath, expected) {
		// Expected commit doesn't exist - critical issue
		return doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   path,
			Description: "Parent expects commit that does not exist in submodule",
			FixCommand:  "Push the missing commit from the original device, then run: git submodule update",
			AutoFixable: false,
			Details: map[string]any{
				"expected": expected,
				"actual":   actual,
				"status":   "missing_commit",
			},
		}
	}

	// Commit exists but HEAD is different - determine if ahead or behind
	if c.isCommitAhead(ctx, fullPath, actual, expected) {
		// Actual is ahead of expected (submodule has local commits)
		return doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Submodule:   path,
			Description: "Submodule is ahead of expected commit (has local commits)",
			FixCommand:  fmt.Sprintf("cd %s && git checkout %s", path, expected[:7]),
			AutoFixable: false, // Manual decision needed
			Details: map[string]any{
				"expected": expected,
				"actual":   actual,
				"status":   "ahead",
			},
		}
	}

	// Actual is behind expected (needs update)
	return doctor.Issue{
		Severity:    doctor.SeverityWarning,
		CheckID:     c.ID(),
		Submodule:   path,
		Description: "Submodule is behind expected commit (needs update)",
		FixCommand:  fmt.Sprintf("git submodule update %s", path),
		AutoFixable: true,
		Details: map[string]any{
			"expected": expected,
			"actual":   actual,
			"status":   "behind",
		},
	}
}
