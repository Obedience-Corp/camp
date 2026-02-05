package clone

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestCloneResult_JSON_Success(t *testing.T) {
	result := &CloneResult{
		Success:   true,
		Directory: "/tmp/test-repo",
		Branch:    "main",
	}

	jsonBytes, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(jsonBytes, &output); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if !output.Success {
		t.Error("JSON success should be true")
	}
	if output.Directory != "/tmp/test-repo" {
		t.Errorf("JSON directory = %q, want /tmp/test-repo", output.Directory)
	}
	if output.Branch != "main" {
		t.Errorf("JSON branch = %q, want main", output.Branch)
	}
}

func TestCloneResult_JSON_WithSubmodules(t *testing.T) {
	result := &CloneResult{
		Success:   true,
		Directory: "/tmp/test-repo",
		Submodules: []SubmoduleResult{
			{Name: "sub1", Path: "path/sub1", URL: "https://example.com/sub1", Success: true, Commit: "abc123"},
			{Name: "sub2", Path: "path/sub2", URL: "https://example.com/sub2", Success: false, Error: errors.New("clone failed")},
		},
	}

	jsonBytes, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(jsonBytes, &output); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if output.Submodules.Total != 2 {
		t.Errorf("Submodules.Total = %d, want 2", output.Submodules.Total)
	}
	if output.Submodules.Initialized != 1 {
		t.Errorf("Submodules.Initialized = %d, want 1", output.Submodules.Initialized)
	}
	if output.Submodules.Failed != 1 {
		t.Errorf("Submodules.Failed = %d, want 1", output.Submodules.Failed)
	}
	if len(output.Submodules.Results) != 2 {
		t.Errorf("Submodules.Results length = %d, want 2", len(output.Submodules.Results))
	}
}

func TestCloneResult_JSON_WithURLChanges(t *testing.T) {
	result := &CloneResult{
		Success:   true,
		Directory: "/tmp/test-repo",
		URLChanges: []URLChange{
			{Submodule: "sub1", OldURL: "https://old.com/sub1", NewURL: "https://new.com/sub1"},
		},
	}

	jsonBytes, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(jsonBytes, &output); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if len(output.URLChanges) != 1 {
		t.Errorf("URLChanges length = %d, want 1", len(output.URLChanges))
	}
	if output.URLChanges[0].Submodule != "sub1" {
		t.Errorf("URLChanges[0].Submodule = %q, want sub1", output.URLChanges[0].Submodule)
	}
}

func TestCloneResult_JSON_WithValidation(t *testing.T) {
	result := &CloneResult{
		Success:   false,
		Directory: "/tmp/test-repo",
		Validation: &ValidationResult{
			Passed:         false,
			AllInitialized: true,
			CorrectCommits: false,
			URLsMatch:      true,
			Issues: []ValidationIssue{
				{CheckID: "commits", Submodule: "sub1", Severity: SeverityWarning, Description: "wrong commit"},
			},
		},
	}

	jsonBytes, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(jsonBytes, &output); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if output.Validation == nil {
		t.Fatal("Validation should not be nil")
	}
	if output.Validation.Passed {
		t.Error("Validation.Passed should be false")
	}
	if !output.Validation.Checks.AllInitialized {
		t.Error("Validation.Checks.AllInitialized should be true")
	}
	if output.Validation.Checks.CorrectCommits {
		t.Error("Validation.Checks.CorrectCommits should be false")
	}
	if len(output.Validation.Issues) != 1 {
		t.Errorf("Validation.Issues length = %d, want 1", len(output.Validation.Issues))
	}
}

func TestCloneResult_JSON_WithErrors(t *testing.T) {
	result := &CloneResult{
		Success:   false,
		Directory: "/tmp/test-repo",
		Errors:    []error{errors.New("error1"), errors.New("error2")},
		Warnings:  []string{"warning1"},
	}

	jsonBytes, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var output JSONOutput
	if err := json.Unmarshal(jsonBytes, &output); err != nil {
		t.Fatalf("JSON output is not valid JSON: %v", err)
	}

	if len(output.Errors) != 2 {
		t.Errorf("Errors length = %d, want 2", len(output.Errors))
	}
	if len(output.Warnings) != 1 {
		t.Errorf("Warnings length = %d, want 1", len(output.Warnings))
	}
}

func TestJSONError(t *testing.T) {
	err := errors.New("test error")
	jsonBytes := JSONError(err)

	var output map[string]any
	if parseErr := json.Unmarshal(jsonBytes, &output); parseErr != nil {
		t.Fatalf("JSONError output is not valid JSON: %v", parseErr)
	}

	if output["success"] != false {
		t.Error("JSONError success should be false")
	}
	if output["error"] != "test error" {
		t.Errorf("JSONError error = %q, want 'test error'", output["error"])
	}
}

func TestCloneResult_Format_Success(t *testing.T) {
	result := &CloneResult{
		Success:   true,
		Directory: "/tmp/test-repo",
		Branch:    "main",
	}

	output := result.Format()

	if !strings.Contains(output, "✓") {
		t.Error("Format should contain success checkmark")
	}
	if !strings.Contains(output, "Campaign cloned successfully") {
		t.Error("Format should contain success message")
	}
	if !strings.Contains(output, "/tmp/test-repo") {
		t.Error("Format should contain directory")
	}
}

func TestCloneResult_Format_Failure(t *testing.T) {
	result := &CloneResult{
		Success:   false,
		Directory: "/tmp/test-repo",
		Errors:    []error{errors.New("test error")},
	}

	output := result.Format()

	if !strings.Contains(output, "✗") {
		t.Error("Format should contain failure mark")
	}
	if !strings.Contains(output, "Clone completed with issues") {
		t.Error("Format should contain failure message")
	}
	if !strings.Contains(output, "test error") {
		t.Error("Format should contain error message")
	}
}

func TestCloneResult_Format_WithValidationIssues(t *testing.T) {
	result := &CloneResult{
		Success:   false,
		Directory: "/tmp/test-repo",
		Validation: &ValidationResult{
			Passed: false,
			Issues: []ValidationIssue{
				{Submodule: "sub1", Severity: SeverityError, Description: "not initialized"},
				{Submodule: "sub2", Severity: SeverityWarning, Description: "wrong commit"},
			},
		},
	}

	output := result.Format()

	if !strings.Contains(output, "Issues detected") {
		t.Error("Format should indicate validation issues")
	}
	if !strings.Contains(output, "sub1") {
		t.Error("Format should contain submodule name")
	}
}
