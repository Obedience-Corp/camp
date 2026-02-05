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

// WorkingCheck detects uncommitted changes in submodules.
type WorkingCheck struct{}

// NewWorkingCheck creates a new working directory check.
func NewWorkingCheck() *WorkingCheck {
	return &WorkingCheck{}
}

// ID returns the check identifier.
func (c *WorkingCheck) ID() string {
	return "working"
}

// Name returns the human-readable check name.
func (c *WorkingCheck) Name() string {
	return "Working Directory"
}

// Description returns a brief explanation of what this check does.
func (c *WorkingCheck) Description() string {
	return "Detects uncommitted changes in submodule working directories"
}

// Run performs the working directory check.
func (c *WorkingCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
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
	cleanCount := 0

	// Check each submodule
	for _, path := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(repoRoot, path)
		changes, err := c.getUncommittedChanges(ctx, fullPath)
		if err != nil {
			// Skip submodules we can't check (maybe not initialized)
			continue
		}

		if len(changes) > 0 {
			result.Passed = false
			result.Issues = append(result.Issues, doctor.Issue{
				Severity:    doctor.SeverityWarning,
				CheckID:     c.ID(),
				Submodule:   path,
				Description: fmt.Sprintf("Has %d uncommitted change(s)", len(changes)),
				FixCommand:  fmt.Sprintf("cd %s && git add . && git commit -m \"WIP\" (or git stash)", path),
				AutoFixable: false,
				Details: map[string]any{
					"changes": changes,
					"count":   len(changes),
				},
			})
		} else {
			cleanCount++
		}
	}

	result.Details["clean"] = cleanCount
	result.Details["dirty"] = len(result.Issues)

	return result, nil
}

// Fix does nothing - working directory issues require manual intervention.
func (c *WorkingCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	// Working directory issues are not auto-fixable
	return nil, nil
}

// listSubmodules returns all submodule paths from .gitmodules.
func (c *WorkingCheck) listSubmodules(ctx context.Context, repoRoot string) ([]string, error) {
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

// getUncommittedChanges returns list of uncommitted changes using git status.
func (c *WorkingCheck) getUncommittedChanges(ctx context.Context, repoPath string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}

	var changes []string
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "XY filename" where X is staged status, Y is working tree status
		changes = append(changes, line)
	}

	return changes, nil
}
