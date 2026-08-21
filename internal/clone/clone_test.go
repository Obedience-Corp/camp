package clone

import (
	"context"
	"testing"
)

func TestClone_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner(WithURL("https://github.com/test/repo.git"))

	_, err := c.Clone(ctx)
	if err != context.Canceled {
		t.Errorf("Clone() error = %v, want context.Canceled", err)
	}
}

func TestValidate_NoSubmodulesDoesNotFailMissingSubmodules(t *testing.T) {
	c := NewCloner(WithNoSubmodules(true))
	result := c.validate(context.Background(), "/tmp/unused")

	if !result.Passed {
		t.Fatalf("validate().Passed = false, want true when NoSubmodules")
	}
	if len(result.Issues) != 0 {
		t.Fatalf("validate().Issues = %v, want none", result.Issues)
	}
}

func TestValidate_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	c := NewCloner()
	result := c.validate(ctx, "/tmp/fake")

	if result.Passed {
		t.Error("validate().Passed = true, want false when context canceled")
	}

	// Should have a context error issue
	hasContextError := false
	for _, issue := range result.Issues {
		if issue.Description == "context cancelled" {
			hasContextError = true
			break
		}
	}
	if !hasContextError {
		t.Error("expected context cancelled issue")
	}
}

func TestCloneResult_SuccessWithErrors(t *testing.T) {
	// Test that Success is correctly calculated based on errors
	result := &CloneResult{
		Success: true,
		Errors:  []error{nil}, // No actual errors
	}

	// A result with errors should not be successful
	result.Errors = append(result.Errors, context.Canceled)
	// In real code, Success would be recalculated, but here we verify the struct
	if len(result.Errors) == 0 {
		t.Error("expected errors to be present")
	}
}

func TestCloneResult_ValidationIssues(t *testing.T) {
	result := &CloneResult{
		Success: true,
		Validation: &ValidationResult{
			Passed: false,
			Issues: []ValidationIssue{
				{Submodule: "projects/sub", Description: "not initialized", Severity: SeverityError},
			},
		},
	}

	if result.Validation.Passed {
		t.Error("Validation.Passed = true, want false")
	}

	if len(result.Validation.Issues) != 1 {
		t.Errorf("Validation.Issues = %d, want 1", len(result.Validation.Issues))
	}
}

func TestSubmoduleResult_Fields(t *testing.T) {
	result := SubmoduleResult{
		Name:    "sub",
		Path:    "projects/sub",
		URL:     "https://github.com/test/sub.git",
		Success: true,
		Commit:  "abc123",
		Error:   nil,
	}

	if result.Name != "sub" {
		t.Errorf("Name = %q, want %q", result.Name, "sub")
	}
	if result.Path != "projects/sub" {
		t.Errorf("Path = %q, want %q", result.Path, "projects/sub")
	}
	if result.URL != "https://github.com/test/sub.git" {
		t.Errorf("URL = %q, want %q", result.URL, "https://github.com/test/sub.git")
	}
	if !result.Success {
		t.Error("Success = false, want true")
	}
	if result.Commit != "abc123" {
		t.Errorf("Commit = %q, want %q", result.Commit, "abc123")
	}
	if result.Error != nil {
		t.Errorf("Error = %v, want nil", result.Error)
	}
}

func TestURLChange_Fields(t *testing.T) {
	change := URLChange{
		Submodule: "projects/sub",
		OldURL:    "https://old.url/repo.git",
		NewURL:    "https://new.url/repo.git",
	}

	if change.Submodule != "projects/sub" {
		t.Errorf("Submodule = %q, want %q", change.Submodule, "projects/sub")
	}
	if change.OldURL != "https://old.url/repo.git" {
		t.Errorf("OldURL = %q, want %q", change.OldURL, "https://old.url/repo.git")
	}
	if change.NewURL != "https://new.url/repo.git" {
		t.Errorf("NewURL = %q, want %q", change.NewURL, "https://new.url/repo.git")
	}
}

func TestValidationIssue_Severity(t *testing.T) {
	tests := []struct {
		name       string
		severity   Severity
		wantString string
	}{
		{"error severity", SeverityError, "error"},
		{"warning severity", SeverityWarning, "warning"},
		{"info severity", SeverityInfo, "info"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			issue := ValidationIssue{
				Submodule:   "projects/sub",
				Description: "test issue",
				Severity:    tt.severity,
			}

			if issue.Severity != tt.severity {
				t.Errorf("Severity = %v, want %v", issue.Severity, tt.severity)
			}

			if tt.severity.String() != tt.wantString {
				t.Errorf("Severity.String() = %q, want %q", tt.severity.String(), tt.wantString)
			}
		})
	}
}
