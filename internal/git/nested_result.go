package git

import (
	"errors"
	"fmt"
)

// NestedResult records one child discovered in its owner's selected checkout.
// Success describes the entire subtree; Initialized describes this child only.
type NestedResult struct {
	Owner            string
	Path             string
	Name             string
	URL              string
	Operation        string
	Attempted        bool
	Skipped          bool
	Initialized      bool
	Success          bool
	RecordedCommit   string
	Commit           string
	Branch           string
	StaleRefFallback bool
	Warning          string
	Error            error
	Children         []NestedResult
}

// NestedError preserves the owner, full display path, operation, and cause.
type NestedError struct {
	Owner string
	Path  string
	Op    string
	Cause error
}

func (e *NestedError) Error() string {
	return fmt.Sprintf("%s %s (owner %s): %v", e.Op, e.Path, e.Owner, e.Cause)
}

func (e *NestedError) Unwrap() error { return e.Cause }

// NestedSummary counts discovered children, including unattempted children
// skipped because the context was cancelled.
type NestedSummary struct {
	Attempted   int `json:"attempted"`
	Skipped     int `json:"skipped"`
	Initialized int `json:"initialized"`
	Failed      int `json:"failed"`
}

// NestedResultOutput is the JSON-safe form of one recursive result.
type NestedResultOutput struct {
	Owner            string               `json:"owner"`
	Path             string               `json:"path"`
	Name             string               `json:"name,omitempty"`
	URL              string               `json:"url,omitempty"`
	Operation        string               `json:"operation,omitempty"`
	Attempted        bool                 `json:"attempted"`
	Skipped          bool                 `json:"skipped"`
	Initialized      bool                 `json:"initialized"`
	Success          bool                 `json:"success"`
	RecordedCommit   string               `json:"recordedCommit,omitempty"`
	Commit           string               `json:"commit,omitempty"`
	Branch           string               `json:"branch,omitempty"`
	StaleRefFallback bool                 `json:"staleRefFallback,omitempty"`
	Warning          string               `json:"warning,omitempty"`
	Error            string               `json:"error,omitempty"`
	Children         []NestedResultOutput `json:"children,omitempty"`
}

func NestedResultOutputs(results []NestedResult) []NestedResultOutput {
	if len(results) == 0 {
		return nil
	}
	out := make([]NestedResultOutput, 0, len(results))
	for _, result := range results {
		item := NestedResultOutput{
			Owner: result.Owner, Path: result.Path, Name: result.Name, URL: result.URL,
			Operation: result.Operation, Attempted: result.Attempted, Skipped: result.Skipped,
			Initialized: result.Initialized, Success: result.Success,
			RecordedCommit: result.RecordedCommit, Commit: result.Commit, Branch: result.Branch,
			StaleRefFallback: result.StaleRefFallback, Warning: result.Warning,
			Children: NestedResultOutputs(result.Children),
		}
		if result.Error != nil {
			item.Error = result.Error.Error()
		}
		out = append(out, item)
	}
	return out
}

func SummarizeNestedResults(results []NestedResult) NestedSummary {
	var summary NestedSummary
	for _, result := range results {
		if result.Attempted {
			summary.Attempted++
		}
		if result.Skipped {
			summary.Skipped++
		}
		if result.Initialized {
			summary.Initialized++
		}
		if result.Attempted && result.Error != nil && result.Operation != "children" {
			summary.Failed++
		}
		child := SummarizeNestedResults(result.Children)
		summary.Attempted += child.Attempted
		summary.Skipped += child.Skipped
		summary.Initialized += child.Initialized
		summary.Failed += child.Failed
	}
	return summary
}

// NestedFailures joins each failed direct child. Child errors already retain
// their own descendants, so callers receive every independent failure once.
func NestedFailures(results []NestedResult) error {
	var failures []error
	for _, result := range results {
		if result.Error != nil {
			failures = append(failures, result.Error)
		}
	}
	return errors.Join(failures...)
}

func NestedWarnings(results []NestedResult) []string {
	var warnings []string
	for _, result := range results {
		if result.Warning != "" {
			warnings = append(warnings, result.Warning)
		}
		warnings = append(warnings, NestedWarnings(result.Children)...)
	}
	return warnings
}
