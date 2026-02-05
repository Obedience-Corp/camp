package clone

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Format returns a human-readable validation report.
func (r *ValidationResult) Format() string {
	var sb strings.Builder

	// Header
	sb.WriteString("Validation Results\n")
	sb.WriteString(strings.Repeat("─", 40) + "\n")

	// Count by severity
	errors := 0
	warnings := 0
	for _, issue := range r.Issues {
		switch issue.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		}
	}

	// Check results summary
	if r.AllInitialized {
		sb.WriteString("  ✓ All submodules initialized\n")
	} else {
		sb.WriteString("  ✗ Some submodules not initialized\n")
	}

	if r.CorrectCommits {
		sb.WriteString("  ✓ All submodules at correct commits\n")
	} else {
		sb.WriteString("  ⚠ Some submodules at different commits\n")
	}

	if r.URLsMatch {
		sb.WriteString("  ✓ No URL mismatches\n")
	} else {
		sb.WriteString("  ⚠ URL mismatches detected\n")
	}

	// Issues detail (if any)
	if len(r.Issues) > 0 {
		sb.WriteString("\nIssues:\n")

		// Errors first
		errorIssues := filterBySeverity(r.Issues, SeverityError)
		for i, issue := range errorIssues {
			formatIssue(&sb, i+1, "ERROR", issue)
		}

		// Then warnings
		warningIssues := filterBySeverity(r.Issues, SeverityWarning)
		for i, issue := range warningIssues {
			formatIssue(&sb, len(errorIssues)+i+1, "WARN", issue)
		}

		// Then info
		infoIssues := filterBySeverity(r.Issues, SeverityInfo)
		for i, issue := range infoIssues {
			formatIssue(&sb, len(errorIssues)+len(warningIssues)+i+1, "INFO", issue)
		}
	}

	// Summary
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", 40) + "\n")
	if r.Passed {
		sb.WriteString("✓ Validation passed\n")
	} else {
		sb.WriteString(fmt.Sprintf("✗ Validation failed: %d errors, %d warnings\n", errors, warnings))
	}

	return sb.String()
}

// formatIssue appends a formatted issue to the string builder.
func formatIssue(sb *strings.Builder, num int, level string, issue ValidationIssue) {
	submodule := issue.Submodule
	if submodule == "" {
		submodule = "(global)"
	}
	sb.WriteString(fmt.Sprintf("  %d. [%s] %s\n", num, level, submodule))
	sb.WriteString(fmt.Sprintf("     %s\n", issue.Description))
	if issue.FixCommand != "" {
		sb.WriteString(fmt.Sprintf("     Fix: %s\n", issue.FixCommand))
	}
}

// JSON returns the validation result as JSON.
func (r *ValidationResult) JSON() ([]byte, error) {
	type jsonIssue struct {
		CheckID     string `json:"checkId"`
		Submodule   string `json:"submodule"`
		Severity    string `json:"severity"`
		Description string `json:"description"`
		FixCommand  string `json:"fixCommand,omitempty"`
		AutoFixable bool   `json:"autoFixable"`
	}

	type jsonResult struct {
		Passed         bool            `json:"passed"`
		AllInitialized bool            `json:"allInitialized"`
		CorrectCommits bool            `json:"correctCommits"`
		URLsMatch      bool            `json:"urlsMatch"`
		Issues         []jsonIssue     `json:"issues"`
		CheckResults   map[string]bool `json:"checkResults,omitempty"`
	}

	issues := make([]jsonIssue, len(r.Issues))
	for i, issue := range r.Issues {
		issues[i] = jsonIssue{
			CheckID:     issue.CheckID,
			Submodule:   issue.Submodule,
			Severity:    issue.Severity.String(),
			Description: issue.Description,
			FixCommand:  issue.FixCommand,
			AutoFixable: issue.AutoFixable,
		}
	}

	result := jsonResult{
		Passed:         r.Passed,
		AllInitialized: r.AllInitialized,
		CorrectCommits: r.CorrectCommits,
		URLsMatch:      r.URLsMatch,
		Issues:         issues,
		CheckResults:   r.CheckResults,
	}

	return json.MarshalIndent(result, "", "  ")
}

// filterBySeverity returns issues matching the given severity.
func filterBySeverity(issues []ValidationIssue, severity Severity) []ValidationIssue {
	var result []ValidationIssue
	for _, issue := range issues {
		if issue.Severity == severity {
			result = append(result, issue)
		}
	}
	return result
}

// ErrorCount returns the number of error-severity issues.
func (r *ValidationResult) ErrorCount() int {
	return len(filterBySeverity(r.Issues, SeverityError))
}

// WarningCount returns the number of warning-severity issues.
func (r *ValidationResult) WarningCount() int {
	return len(filterBySeverity(r.Issues, SeverityWarning))
}

// AutoFixableIssues returns issues that can be automatically fixed.
func (r *ValidationResult) AutoFixableIssues() []ValidationIssue {
	var result []ValidationIssue
	for _, issue := range r.Issues {
		if issue.AutoFixable {
			result = append(result, issue)
		}
	}
	return result
}

// FormatSSHError returns user-friendly SSH error guidance.
func FormatSSHError(err error) string {
	errStr := err.Error()
	if strings.Contains(errStr, "Permission denied (publickey)") {
		return `SSH key not configured for GitHub.

To verify: ssh -T git@github.com
To set up: https://docs.github.com/en/authentication/connecting-to-github-with-ssh

Alternative: Clone via HTTPS instead of SSH`
	}
	if strings.Contains(errStr, "Host key verification failed") {
		return `GitHub host key not verified.

To fix: ssh-keyscan github.com >> ~/.ssh/known_hosts`
	}
	return errStr
}
