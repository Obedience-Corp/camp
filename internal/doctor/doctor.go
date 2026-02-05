package doctor

import (
	"context"
)

// Run performs health checks on the campaign.
// It runs all configured checks and optionally attempts fixes.
func (d *Doctor) Run(ctx context.Context) (*DoctorResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &DoctorResult{
		Success:      true,
		CheckResults: make(map[string]bool),
	}

	// Filter checks if specific checks requested
	checksToRun := d.checks
	if len(d.options.Checks) > 0 {
		checksToRun = d.filterChecks(d.options.Checks)
	}

	// Run each check
	for _, check := range checksToRun {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		checkResult, err := check.Run(ctx, d.repoRoot)
		if err != nil {
			// Graceful degradation - record error but continue with other checks
			result.Issues = append(result.Issues, Issue{
				Severity:    SeverityError,
				CheckID:     check.ID(),
				Description: "Check execution failed: " + err.Error(),
				AutoFixable: false,
			})
			result.Failed++
			result.Success = false
			result.CheckResults[check.ID()] = false
			continue
		}

		// Categorize results
		hasError := false
		hasWarning := false
		for _, issue := range checkResult.Issues {
			result.Issues = append(result.Issues, issue)
			if issue.Severity == SeverityError {
				hasError = true
			} else if issue.Severity == SeverityWarning {
				hasWarning = true
			}
		}

		// Update counters
		if hasError {
			result.Failed++
			result.Success = false
			result.CheckResults[check.ID()] = false
		} else if hasWarning {
			result.Warned++
			result.CheckResults[check.ID()] = true
		} else {
			result.Passed++
			result.CheckResults[check.ID()] = true
		}
	}

	// Attempt fixes if requested
	if d.options.Fix && len(result.Issues) > 0 {
		result.Fixed = d.attemptFixes(ctx, checksToRun, result.Issues)

		// Re-check success after fixes
		unfixedErrors := 0
		for _, issue := range result.Issues {
			if issue.Severity == SeverityError && !isFixed(issue, result.Fixed) {
				unfixedErrors++
			}
		}
		result.Success = unfixedErrors == 0
	}

	return result, nil
}

// filterChecks returns only checks matching the requested IDs.
func (d *Doctor) filterChecks(ids []string) []Check {
	idSet := make(map[string]bool)
	for _, id := range ids {
		idSet[id] = true
	}

	var filtered []Check
	for _, check := range d.checks {
		if idSet[check.ID()] {
			filtered = append(filtered, check)
		}
	}
	return filtered
}

// attemptFixes tries to fix all fixable issues.
func (d *Doctor) attemptFixes(ctx context.Context, checks []Check, issues []Issue) []Issue {
	var fixed []Issue

	// Group issues by check ID
	byCheck := make(map[string][]Issue)
	for _, issue := range issues {
		if issue.AutoFixable {
			byCheck[issue.CheckID] = append(byCheck[issue.CheckID], issue)
		}
	}

	// Run fixes for each check
	for _, check := range checks {
		checkIssues := byCheck[check.ID()]
		if len(checkIssues) == 0 {
			continue
		}

		if ctx.Err() != nil {
			break
		}

		fixedIssues, err := check.Fix(ctx, d.repoRoot, checkIssues)
		if err == nil {
			fixed = append(fixed, fixedIssues...)
		}
	}

	return fixed
}

// isFixed checks if an issue was successfully fixed.
func isFixed(issue Issue, fixed []Issue) bool {
	for _, f := range fixed {
		if f.CheckID == issue.CheckID && f.Submodule == issue.Submodule && f.Description == issue.Description {
			return true
		}
	}
	return false
}
