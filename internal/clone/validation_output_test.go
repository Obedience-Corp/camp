package clone

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestValidationResult_Format_AllPassed(t *testing.T) {
	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: true,
		URLsMatch:      true,
		Issues:         nil,
	}

	output := result.Format()

	// Should contain success indicators
	if !strings.Contains(output, "✓ All submodules initialized") {
		t.Error("Format() should show initialized success")
	}
	if !strings.Contains(output, "✓ All submodules at correct commits") {
		t.Error("Format() should show commits success")
	}
	if !strings.Contains(output, "✓ No URL mismatches") {
		t.Error("Format() should show URL success")
	}
	if !strings.Contains(output, "✓ Validation passed") {
		t.Error("Format() should show overall success")
	}
}

func TestValidationResult_Format_WithErrors(t *testing.T) {
	result := &ValidationResult{
		Passed:         false,
		AllInitialized: false,
		CorrectCommits: true,
		URLsMatch:      true,
		Issues: []ValidationIssue{
			{
				CheckID:     "initialized",
				Submodule:   "projects/sub1",
				Severity:    SeverityError,
				Description: "Submodule not initialized",
				FixCommand:  "git submodule update --init projects/sub1",
			},
		},
	}

	output := result.Format()

	// Should show failure
	if !strings.Contains(output, "✗ Some submodules not initialized") {
		t.Error("Format() should show initialized failure")
	}
	if !strings.Contains(output, "[ERROR]") {
		t.Error("Format() should show error label")
	}
	if !strings.Contains(output, "projects/sub1") {
		t.Error("Format() should show submodule path")
	}
	if !strings.Contains(output, "git submodule update") {
		t.Error("Format() should show fix command")
	}
	if !strings.Contains(output, "✗ Validation failed") {
		t.Error("Format() should show overall failure")
	}
}

func TestValidationResult_Format_WithWarnings(t *testing.T) {
	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: false,
		URLsMatch:      true,
		Issues: []ValidationIssue{
			{
				CheckID:     "commits",
				Submodule:   "projects/sub1",
				Severity:    SeverityWarning,
				Description: "Submodule at different commit",
			},
		},
	}

	output := result.Format()

	if !strings.Contains(output, "[WARN]") {
		t.Error("Format() should show warning label")
	}
	if !strings.Contains(output, "⚠ Some submodules at different commits") {
		t.Error("Format() should show commits warning")
	}
}

func TestValidationResult_Format_GroupsBySeverity(t *testing.T) {
	result := &ValidationResult{
		Passed: false,
		Issues: []ValidationIssue{
			{Severity: SeverityWarning, Description: "Warning 1", Submodule: "sub-warn"},
			{Severity: SeverityError, Description: "Error 1", Submodule: "sub-error"},
			{Severity: SeverityInfo, Description: "Info 1", Submodule: "sub-info"},
		},
	}

	output := result.Format()

	// Errors should appear before warnings
	errorPos := strings.Index(output, "[ERROR]")
	warnPos := strings.Index(output, "[WARN]")
	infoPos := strings.Index(output, "[INFO]")

	if errorPos > warnPos {
		t.Error("Errors should appear before warnings")
	}
	if warnPos > infoPos {
		t.Error("Warnings should appear before info")
	}
}

func TestValidationResult_JSON_AllPassed(t *testing.T) {
	result := &ValidationResult{
		Passed:         true,
		AllInitialized: true,
		CorrectCommits: true,
		URLsMatch:      true,
		Issues:         nil,
	}

	data, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON() returned invalid JSON: %v", err)
	}

	if passed, ok := parsed["passed"].(bool); !ok || !passed {
		t.Error("JSON() should have passed=true")
	}
}

func TestValidationResult_JSON_WithIssues(t *testing.T) {
	result := &ValidationResult{
		Passed: false,
		Issues: []ValidationIssue{
			{
				CheckID:     "test",
				Submodule:   "projects/sub",
				Severity:    SeverityError,
				Description: "Test error",
				FixCommand:  "git fix",
				AutoFixable: true,
			},
		},
	}

	data, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("JSON() returned invalid JSON: %v", err)
	}

	issues, ok := parsed["issues"].([]interface{})
	if !ok {
		t.Fatal("JSON() should have issues array")
	}
	if len(issues) != 1 {
		t.Errorf("JSON() issues = %d, want 1", len(issues))
	}

	issue := issues[0].(map[string]interface{})
	if issue["severity"] != "error" {
		t.Errorf("JSON() issue severity = %v, want 'error'", issue["severity"])
	}
	if issue["autoFixable"] != true {
		t.Errorf("JSON() issue autoFixable = %v, want true", issue["autoFixable"])
	}
}

func TestFilterBySeverity(t *testing.T) {
	issues := []ValidationIssue{
		{Severity: SeverityError, Description: "Error 1"},
		{Severity: SeverityWarning, Description: "Warning 1"},
		{Severity: SeverityError, Description: "Error 2"},
		{Severity: SeverityInfo, Description: "Info 1"},
	}

	errors := filterBySeverity(issues, SeverityError)
	if len(errors) != 2 {
		t.Errorf("filterBySeverity(Error) = %d issues, want 2", len(errors))
	}

	warnings := filterBySeverity(issues, SeverityWarning)
	if len(warnings) != 1 {
		t.Errorf("filterBySeverity(Warning) = %d issues, want 1", len(warnings))
	}

	infos := filterBySeverity(issues, SeverityInfo)
	if len(infos) != 1 {
		t.Errorf("filterBySeverity(Info) = %d issues, want 1", len(infos))
	}
}

func TestValidationResult_ErrorCount(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{Severity: SeverityError},
			{Severity: SeverityWarning},
			{Severity: SeverityError},
		},
	}

	if result.ErrorCount() != 2 {
		t.Errorf("ErrorCount() = %d, want 2", result.ErrorCount())
	}
}

func TestValidationResult_WarningCount(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{Severity: SeverityError},
			{Severity: SeverityWarning},
			{Severity: SeverityWarning},
		},
	}

	if result.WarningCount() != 2 {
		t.Errorf("WarningCount() = %d, want 2", result.WarningCount())
	}
}

func TestValidationResult_AutoFixableIssues(t *testing.T) {
	result := &ValidationResult{
		Issues: []ValidationIssue{
			{AutoFixable: true, Description: "Fixable"},
			{AutoFixable: false, Description: "Not fixable"},
			{AutoFixable: true, Description: "Also fixable"},
		},
	}

	fixable := result.AutoFixableIssues()
	if len(fixable) != 2 {
		t.Errorf("AutoFixableIssues() = %d, want 2", len(fixable))
	}
}

func TestFormatSSHError_PublicKey(t *testing.T) {
	err := errors.New("Permission denied (publickey)")
	result := FormatSSHError(err)

	if !strings.Contains(result, "SSH key not configured") {
		t.Error("FormatSSHError should mention SSH key configuration")
	}
	if !strings.Contains(result, "ssh -T git@github.com") {
		t.Error("FormatSSHError should include verification command")
	}
}

func TestFormatSSHError_HostKey(t *testing.T) {
	err := errors.New("Host key verification failed")
	result := FormatSSHError(err)

	if !strings.Contains(result, "host key not verified") {
		t.Error("FormatSSHError should mention host key verification")
	}
	if !strings.Contains(result, "ssh-keyscan") {
		t.Error("FormatSSHError should include fix command")
	}
}

func TestFormatSSHError_Other(t *testing.T) {
	err := errors.New("Some other error")
	result := FormatSSHError(err)

	if result != "Some other error" {
		t.Errorf("FormatSSHError() = %q, want original error", result)
	}
}
