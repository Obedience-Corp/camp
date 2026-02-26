package checks

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/doctor"
	"github.com/Obedience-Corp/camp/internal/git"
)

// URLCheck verifies URL consistency between .gitmodules and .git/config.
type URLCheck struct{}

// NewURLCheck creates a new URL consistency check.
func NewURLCheck() *URLCheck {
	return &URLCheck{}
}

// ID returns the check identifier.
func (c *URLCheck) ID() string {
	return "url"
}

// Name returns the human-readable check name.
func (c *URLCheck) Name() string {
	return "URL Consistency"
}

// Description returns a brief explanation of what this check does.
func (c *URLCheck) Description() string {
	return "Verifies that submodule URLs in .gitmodules match .git/config"
}

// Run performs the URL consistency check.
func (c *URLCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Total:   0,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	// Get list of submodules
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

		comparison, err := git.CompareURLs(ctx, repoRoot, path)
		if err != nil {
			// Skip submodules with URL issues
			continue
		}

		// Only report mismatch if active URL exists (submodule is initialized)
		if !comparison.Match && comparison.ActiveURL != "" {
			result.Passed = false
			result.Issues = append(result.Issues, doctor.Issue{
				Severity:    doctor.SeverityWarning,
				CheckID:     c.ID(),
				Submodule:   path,
				Description: fmt.Sprintf("URL mismatch: .gitmodules=%s, .git/config=%s", comparison.DeclaredURL, comparison.ActiveURL),
				FixCommand:  "git submodule sync --recursive",
				AutoFixable: true,
				Details: map[string]any{
					"declared": comparison.DeclaredURL,
					"active":   comparison.ActiveURL,
				},
			})
		}
	}

	return result, nil
}

// Fix attempts to repair URL mismatches by running git submodule sync.
func (c *URLCheck) Fix(ctx context.Context, repoRoot string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(issues) == 0 {
		return nil, nil
	}

	// Run git submodule sync --recursive to fix all URL mismatches at once
	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "submodule", "sync", "--recursive")
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git submodule sync: %w", err)
	}

	// All issues should be fixed after sync
	return issues, nil
}

// listSubmodules returns all submodule paths from .gitmodules.
func (c *URLCheck) listSubmodules(ctx context.Context, repoRoot string) ([]string, error) {
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
		// Format: submodule.path/to/sub.path projects/sub
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			paths = append(paths, parts[1])
		}
	}

	return paths, nil
}
