package clone

import (
	"context"
	"testing"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		severity Severity
		want     string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{Severity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.want {
				t.Errorf("Severity.String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNewValidator(t *testing.T) {
	v := NewValidator()
	if v == nil {
		t.Fatal("NewValidator() returned nil")
	}
	if len(v.checks) != 0 {
		t.Errorf("NewValidator().checks has %d items, want 0", len(v.checks))
	}
}

func TestDefaultValidator_RegisterCheck(t *testing.T) {
	v := NewValidator()

	check := &mockCheck{id: "test-check", name: "Test Check"}
	v.RegisterCheck(check)

	if len(v.checks) != 1 {
		t.Errorf("RegisterCheck() did not add check, got %d checks", len(v.checks))
	}

	if v.checks[0].ID() != "test-check" {
		t.Errorf("RegisterCheck() added wrong check, got ID %q", v.checks[0].ID())
	}
}

func TestDefaultValidator_Checks(t *testing.T) {
	v := NewValidator()

	check1 := &mockCheck{id: "check1", name: "Check 1"}
	check2 := &mockCheck{id: "check2", name: "Check 2"}
	v.RegisterCheck(check1)
	v.RegisterCheck(check2)

	checks := v.Checks()
	if len(checks) != 2 {
		t.Errorf("Checks() returned %d checks, want 2", len(checks))
	}
}

func TestDefaultValidator_Validate_NoChecks(t *testing.T) {
	ctx := context.Background()
	v := NewValidator()

	result, err := v.Validate(ctx, "/tmp/fake")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !result.Passed {
		t.Error("Validate() with no checks should pass")
	}

	if len(result.Issues) != 0 {
		t.Errorf("Validate() with no checks returned %d issues", len(result.Issues))
	}
}

func TestDefaultValidator_Validate_AllPass(t *testing.T) {
	ctx := context.Background()
	v := NewValidator()

	check := &mockCheck{id: "pass-check", name: "Pass Check", issues: nil}
	v.RegisterCheck(check)

	result, err := v.Validate(ctx, "/tmp/fake")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if !result.Passed {
		t.Error("Validate() should pass when all checks pass")
	}

	if !result.CheckResults["pass-check"] {
		t.Error("CheckResults should mark pass-check as passed")
	}
}

func TestDefaultValidator_Validate_WithErrors(t *testing.T) {
	ctx := context.Background()
	v := NewValidator()

	check := &mockCheck{
		id:   "error-check",
		name: "Error Check",
		issues: []ValidationIssue{
			{CheckID: "error-check", Severity: SeverityError, Description: "Test error"},
		},
	}
	v.RegisterCheck(check)

	result, err := v.Validate(ctx, "/tmp/fake")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	if result.Passed {
		t.Error("Validate() should fail when checks have errors")
	}

	if result.CheckResults["error-check"] {
		t.Error("CheckResults should mark error-check as failed")
	}
}

func TestDefaultValidator_Validate_WithWarnings(t *testing.T) {
	ctx := context.Background()
	v := NewValidator()

	check := &mockCheck{
		id:   "warn-check",
		name: "Warning Check",
		issues: []ValidationIssue{
			{CheckID: "warn-check", Severity: SeverityWarning, Description: "Test warning"},
		},
	}
	v.RegisterCheck(check)

	result, err := v.Validate(ctx, "/tmp/fake")
	if err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	// Warnings don't cause failure
	if !result.Passed {
		t.Error("Validate() should pass with only warnings")
	}

	if !result.CheckResults["warn-check"] {
		t.Error("CheckResults should mark warn-check as passed (warnings don't fail)")
	}
}

func TestDefaultValidator_Validate_ContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	v := NewValidator()
	_, err := v.Validate(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("Validate() error = %v, want context.Canceled", err)
	}
}

func TestDefaultValidator_Validate_ContextCanceledDuringCheck(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	v := NewValidator()
	// First check cancels the context
	check1 := &mockCheck{
		id:   "cancel-check",
		name: "Cancel Check",
		runFunc: func() ([]ValidationIssue, error) {
			cancel()
			return nil, nil
		},
	}
	// Second check would run after, but context should be checked first
	check2 := &mockCheck{
		id:   "after-check",
		name: "After Check",
	}
	v.RegisterCheck(check1)
	v.RegisterCheck(check2)

	_, err := v.Validate(ctx, "/tmp/fake")
	if err != context.Canceled {
		t.Errorf("Validate() error = %v, want context.Canceled", err)
	}
}

// mockCheck implements ValidationCheck for testing
type mockCheck struct {
	id      string
	name    string
	issues  []ValidationIssue
	runFunc func() ([]ValidationIssue, error)
}

func (c *mockCheck) ID() string   { return c.id }
func (c *mockCheck) Name() string { return c.name }

func (c *mockCheck) Run(ctx context.Context, repoPath string) ([]ValidationIssue, error) {
	if c.runFunc != nil {
		return c.runFunc()
	}
	return c.issues, nil
}
