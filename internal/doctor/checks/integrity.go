package checks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/doctor"
)

// IntegrityCheck verifies submodule directories are not empty or broken.
type IntegrityCheck struct{}

// NewIntegrityCheck creates a new submodule integrity check.
func NewIntegrityCheck() *IntegrityCheck {
	return &IntegrityCheck{}
}

// ID returns the check identifier.
func (c *IntegrityCheck) ID() string {
	return "integrity"
}

// Name returns the human-readable check name.
func (c *IntegrityCheck) Name() string {
	return "Submodule Integrity"
}

// Description returns a brief explanation of what this check does.
func (c *IntegrityCheck) Description() string {
	return "Verifies that submodule directories exist and are properly initialized"
}

// Run performs the integrity check.
func (c *IntegrityCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
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
	submodules, err := c.getSubmodulePaths(ctx, repoRoot)
	if err != nil {
		return nil, fmt.Errorf("list submodules: %w", err)
	}

	result.Total = len(submodules)

	// Check each submodule
	for name, path := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(repoRoot, path)
		issue := c.checkSubmoduleIntegrity(name, path, fullPath)
		if issue != nil {
			result.Passed = false
			result.Issues = append(result.Issues, *issue)
		}
	}

	return result, nil
}

// Fix attempts to repair integrity issues by initializing submodules.
func (c *IntegrityCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
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

		// Extract submodule path from issue
		submodulePath := issue.Submodule

		// Run git submodule update --init for this specific submodule
		cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "submodule", "update", "--init", submodulePath)
		if err := cmd.Run(); err != nil {
			continue // Skip if fix fails
		}

		fixed = append(fixed, issue)
	}

	return fixed, nil
}

// getSubmodulePaths returns map of submodule name to path.
func (c *IntegrityCheck) getSubmodulePaths(ctx context.Context, repoRoot string) (map[string]string, error) {
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

	paths := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: submodule.NAME.path VALUE
		parts := strings.SplitN(line, " ", 2)
		if len(parts) < 2 {
			continue
		}
		// Extract name from submodule.NAME.path
		keyParts := strings.Split(parts[0], ".")
		if len(keyParts) >= 2 {
			name := keyParts[1]
			path := parts[1]
			paths[name] = path
		}
	}
	return paths, nil
}

// checkSubmoduleIntegrity checks a single submodule's integrity.
func (c *IntegrityCheck) checkSubmoduleIntegrity(name, relPath, fullPath string) *doctor.Issue {
	// Check if directory exists
	info, err := os.Stat(fullPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &doctor.Issue{
				Severity:    doctor.SeverityError,
				CheckID:     c.ID(),
				Submodule:   relPath,
				Description: fmt.Sprintf("Submodule directory does not exist: %s", relPath),
				FixCommand:  fmt.Sprintf("git submodule update --init %s", relPath),
				AutoFixable: true,
				Details: map[string]any{
					"name": name,
					"type": "missing_directory",
				},
			}
		}
		return &doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   relPath,
			Description: fmt.Sprintf("Cannot access submodule directory: %v", err),
			FixCommand:  fmt.Sprintf("git submodule update --init %s", relPath),
			AutoFixable: false,
			Details: map[string]any{
				"name":  name,
				"type":  "access_error",
				"error": err.Error(),
			},
		}
	}

	if !info.IsDir() {
		return &doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   relPath,
			Description: fmt.Sprintf("Submodule path is not a directory: %s", relPath),
			FixCommand:  fmt.Sprintf("rm %s && git submodule update --init %s", relPath, relPath),
			AutoFixable: false,
			Details: map[string]any{
				"name": name,
				"type": "not_directory",
			},
		}
	}

	// Check for .git reference
	gitPath := filepath.Join(fullPath, ".git")
	if _, err := os.Stat(gitPath); err != nil {
		return &doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   relPath,
			Description: fmt.Sprintf("Submodule not initialized (missing .git): %s", relPath),
			FixCommand:  fmt.Sprintf("git submodule update --init %s", relPath),
			AutoFixable: true,
			Details: map[string]any{
				"name": name,
				"type": "not_initialized",
			},
		}
	}

	// Check if directory is empty (except for .git)
	entries, err := os.ReadDir(fullPath)
	if err != nil {
		return &doctor.Issue{
			Severity:    doctor.SeverityError,
			CheckID:     c.ID(),
			Submodule:   relPath,
			Description: fmt.Sprintf("Cannot read submodule directory: %v", err),
			FixCommand:  fmt.Sprintf("git submodule update --init %s", relPath),
			AutoFixable: false,
			Details: map[string]any{
				"name":  name,
				"type":  "read_error",
				"error": err.Error(),
			},
		}
	}

	// Count non-.git entries
	nonGitCount := 0
	for _, entry := range entries {
		if entry.Name() != ".git" {
			nonGitCount++
		}
	}

	if nonGitCount == 0 {
		return &doctor.Issue{
			Severity:    doctor.SeverityWarning,
			CheckID:     c.ID(),
			Submodule:   relPath,
			Description: fmt.Sprintf("Submodule directory is empty: %s", relPath),
			FixCommand:  fmt.Sprintf("git submodule update %s", relPath),
			AutoFixable: true,
			Details: map[string]any{
				"name": name,
				"type": "empty_directory",
			},
		}
	}

	return nil // All good
}
