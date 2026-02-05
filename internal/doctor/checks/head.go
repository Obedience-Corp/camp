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

// HeadCheck identifies detached HEAD states with unpushed commits.
type HeadCheck struct{}

// NewHeadCheck creates a new HEAD state check.
func NewHeadCheck() *HeadCheck {
	return &HeadCheck{}
}

// ID returns the check identifier.
func (c *HeadCheck) ID() string {
	return "head"
}

// Name returns the human-readable check name.
func (c *HeadCheck) Name() string {
	return "HEAD State"
}

// Description returns a brief explanation of what this check does.
func (c *HeadCheck) Description() string {
	return "Identifies detached HEAD states with unpushed commits that could be lost"
}

// Run performs the HEAD state check.
func (c *HeadCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
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

	// Check each submodule
	for _, path := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(repoRoot, path)
		issue := c.checkHEADState(ctx, path, fullPath)
		if issue != nil {
			// HEAD check doesn't fail overall (info/warning only)
			// unless there are unpushed commits (warning level)
			if issue.Severity == doctor.SeverityWarning {
				result.Passed = false
			}
			result.Issues = append(result.Issues, *issue)
		}
	}

	return result, nil
}

// Fix does nothing - HEAD state issues require manual intervention.
func (c *HeadCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	// HEAD issues are not auto-fixable
	return nil, nil
}

// listSubmodules returns all submodule paths from .gitmodules.
func (c *HeadCheck) listSubmodules(ctx context.Context, repoRoot string) ([]string, error) {
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

// checkHEADState checks if submodule has detached HEAD with local commits.
func (c *HeadCheck) checkHEADState(ctx context.Context, relPath, fullPath string) *doctor.Issue {
	// Check if HEAD is detached
	cmd := exec.CommandContext(ctx, "git", "-C", fullPath, "symbolic-ref", "-q", "HEAD")
	err := cmd.Run()

	if err == nil {
		// HEAD is on a branch, all good
		return nil
	}

	// HEAD is detached - check for orphan commits
	cmd = exec.CommandContext(ctx, "git", "-C", fullPath,
		"log", "--oneline", "HEAD", "--not", "--branches", "--remotes")
	output, err := cmd.Output()
	if err != nil {
		// Can't determine commit status, skip
		return nil
	}

	commits := strings.TrimSpace(string(output))
	if commits == "" {
		// Detached but no orphan commits - safe, no issue to report
		return nil
	}

	// Detached with orphan commits - warning!
	commitLines := strings.Split(commits, "\n")
	return &doctor.Issue{
		Severity:    doctor.SeverityWarning,
		CheckID:     c.ID(),
		Submodule:   relPath,
		Description: fmt.Sprintf("Detached HEAD with %d unpushed commit(s) that may be lost", len(commitLines)),
		FixCommand:  fmt.Sprintf("cd %s && git checkout main && git merge HEAD@{1}", relPath),
		AutoFixable: false,
		Details: map[string]any{
			"commits":      commitLines,
			"commit_count": len(commitLines),
		},
	}
}
