package clone

import (
	"encoding/json"
	"fmt"
	"strings"
)

// JSONOutput represents the complete clone result for JSON output.
type JSONOutput struct {
	Success    bool              `json:"success"`
	Directory  string            `json:"directory"`
	Branch     string            `json:"branch,omitempty"`
	Clone      ClonePhaseOutput  `json:"clone"`
	Submodules SubmodulesOutput  `json:"submodules"`
	URLChanges []URLChangeOutput `json:"urlChanges,omitempty"`
	Validation *JSONValidation   `json:"validation,omitempty"`
	Errors     []string          `json:"errors,omitempty"`
	Warnings   []string          `json:"warnings,omitempty"`
}

// ClonePhaseOutput represents the clone phase result.
type ClonePhaseOutput struct {
	Success bool `json:"success"`
}

// SubmodulesOutput represents submodule initialization results.
type SubmodulesOutput struct {
	Total       int                   `json:"total"`
	Initialized int                   `json:"initialized"`
	Failed      int                   `json:"failed"`
	Results     []SubmoduleJSONResult `json:"results,omitempty"`
}

// SubmoduleJSONResult represents a single submodule result.
type SubmoduleJSONResult struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	URL     string `json:"url"`
	Success bool   `json:"success"`
	Commit  string `json:"commit,omitempty"`
	Error   string `json:"error,omitempty"`
}

// URLChangeOutput represents a URL that was updated.
type URLChangeOutput struct {
	Submodule string `json:"submodule"`
	OldURL    string `json:"oldUrl"`
	NewURL    string `json:"newUrl"`
}

// JSONValidation represents validation results in JSON.
type JSONValidation struct {
	Passed bool             `json:"passed"`
	Checks ValidationChecks `json:"checks"`
	Issues []JSONIssue      `json:"issues,omitempty"`
}

// ValidationChecks contains per-check results.
type ValidationChecks struct {
	AllInitialized bool `json:"allInitialized"`
	CorrectCommits bool `json:"correctCommits"`
	URLsMatch      bool `json:"urlsMatch"`
}

// JSONIssue represents a validation issue in JSON.
type JSONIssue struct {
	CheckID     string `json:"checkId,omitempty"`
	Submodule   string `json:"submodule,omitempty"`
	Severity    string `json:"severity"`
	Description string `json:"description"`
	FixCommand  string `json:"fixCommand,omitempty"`
	AutoFixable bool   `json:"autoFixable,omitempty"`
}

// JSON converts CloneResult to JSON bytes.
func (r *CloneResult) JSON() ([]byte, error) {
	output := JSONOutput{
		Success:   r.Success,
		Directory: r.Directory,
		Branch:    r.Branch,
		Clone: ClonePhaseOutput{
			Success: r.Directory != "",
		},
		Warnings: r.Warnings,
	}

	// Convert errors to strings
	for _, err := range r.Errors {
		output.Errors = append(output.Errors, err.Error())
	}

	// Convert submodule results
	initialized := 0
	failed := 0
	var subResults []SubmoduleJSONResult
	for _, sub := range r.Submodules {
		result := SubmoduleJSONResult{
			Name:    sub.Name,
			Path:    sub.Path,
			URL:     sub.URL,
			Success: sub.Success,
			Commit:  sub.Commit,
		}
		if sub.Error != nil {
			result.Error = sub.Error.Error()
			failed++
		} else if sub.Success {
			initialized++
		}
		subResults = append(subResults, result)
	}

	output.Submodules = SubmodulesOutput{
		Total:       len(r.Submodules),
		Initialized: initialized,
		Failed:      failed,
		Results:     subResults,
	}

	// Convert URL changes
	for _, change := range r.URLChanges {
		output.URLChanges = append(output.URLChanges, URLChangeOutput{
			Submodule: change.Submodule,
			OldURL:    change.OldURL,
			NewURL:    change.NewURL,
		})
	}

	// Convert validation
	if r.Validation != nil {
		output.Validation = &JSONValidation{
			Passed: r.Validation.Passed,
			Checks: ValidationChecks{
				AllInitialized: r.Validation.AllInitialized,
				CorrectCommits: r.Validation.CorrectCommits,
				URLsMatch:      r.Validation.URLsMatch,
			},
		}
		for _, issue := range r.Validation.Issues {
			output.Validation.Issues = append(output.Validation.Issues, JSONIssue{
				CheckID:     issue.CheckID,
				Submodule:   issue.Submodule,
				Severity:    issue.Severity.String(),
				Description: issue.Description,
				FixCommand:  issue.FixCommand,
				AutoFixable: issue.AutoFixable,
			})
		}
	}

	return json.MarshalIndent(output, "", "  ")
}

// JSONError creates a JSON error response.
func JSONError(err error) []byte {
	type errorOutput struct {
		Success bool   `json:"success"`
		Error   string `json:"error"`
	}

	output := errorOutput{
		Success: false,
		Error:   err.Error(),
	}

	bytes, _ := json.MarshalIndent(output, "", "  ")
	return bytes
}

// Format returns a human-readable clone result summary.
func (r *CloneResult) Format() string {
	var sb strings.Builder

	sb.WriteString("\n")

	// Header
	if r.Success {
		sb.WriteString("✓ Campaign cloned successfully\n")
	} else {
		sb.WriteString("✗ Clone completed with issues\n")
	}
	sb.WriteString("\n")

	// Directory and branch
	sb.WriteString(fmt.Sprintf("  Directory: %s\n", r.Directory))
	if r.Branch != "" {
		sb.WriteString(fmt.Sprintf("  Branch:    %s\n", r.Branch))
	}
	sb.WriteString("\n")

	// Submodules section
	if len(r.Submodules) > 0 {
		successCount := 0
		for _, sub := range r.Submodules {
			if sub.Success {
				successCount++
			}
		}
		sb.WriteString(fmt.Sprintf("Submodules: %d/%d initialized\n", successCount, len(r.Submodules)))
		sb.WriteString("\n")
	}

	// URL changes section
	if len(r.URLChanges) > 0 {
		sb.WriteString(fmt.Sprintf("URL Synchronization: %d URLs updated\n", len(r.URLChanges)))
		sb.WriteString("\n")
	}

	// Validation section
	if r.Validation != nil {
		if r.Validation.Passed {
			sb.WriteString("Validation: ✓ All checks passed\n")
		} else {
			sb.WriteString("Validation: ✗ Issues detected\n")
			for _, issue := range r.Validation.Issues {
				var icon string
				switch issue.Severity {
				case SeverityError:
					icon = "✗"
				case SeverityWarning:
					icon = "⚠"
				default:
					icon = "ℹ"
				}
				sb.WriteString(fmt.Sprintf("  %s [%s] %s\n", icon, issue.Submodule, issue.Description))
			}
		}
		sb.WriteString("\n")
	}

	// Warnings section
	if len(r.Warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, warning := range r.Warnings {
			sb.WriteString(fmt.Sprintf("  ⚠ %s\n", warning))
		}
		sb.WriteString("\n")
	}

	// Errors section
	if len(r.Errors) > 0 {
		sb.WriteString("Errors:\n")
		for _, err := range r.Errors {
			sb.WriteString(fmt.Sprintf("  ✗ %s\n", err.Error()))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}
