package clone

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// URLMatchCheck verifies .gitmodules URLs match .git/config.
type URLMatchCheck struct{}

// ID returns the unique identifier for this check.
func (c *URLMatchCheck) ID() string { return "urls" }

// Name returns the human-readable name.
func (c *URLMatchCheck) Name() string { return "URL Consistency" }

// Run checks that URLs in .gitmodules match the active URLs in .git/config.
func (c *URLMatchCheck) Run(ctx context.Context, repoPath string) ([]ValidationIssue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var issues []ValidationIssue

	// Get declared URLs from .gitmodules
	submodules, err := parseGitmodules(ctx, repoPath)
	if err != nil {
		return nil, err
	}

	for _, sub := range submodules {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Get active URL from .git/config
		cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "config",
			"--get", fmt.Sprintf("submodule.%s.url", sub.Name))
		output, err := cmd.Output()
		if err != nil {
			// No URL in config - not initialized yet, skip URL check
			continue
		}

		activeURL := strings.TrimSpace(string(output))
		if activeURL != sub.URL {
			issues = append(issues, ValidationIssue{
				CheckID:     c.ID(),
				Submodule:   sub.Path,
				Severity:    SeverityWarning,
				Description: fmt.Sprintf("URL mismatch: .gitmodules=%s, .git/config=%s", sub.URL, activeURL),
				FixCommand:  "git submodule sync " + sub.Path,
				AutoFixable: true,
			})
		}
	}

	return issues, nil
}
