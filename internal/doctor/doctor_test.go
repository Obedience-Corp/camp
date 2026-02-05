package doctor

import (
	"context"
	"errors"
	"testing"
)

// mockCheck is a test helper that implements the Check interface.
type mockCheck struct {
	id          string
	name        string
	description string
	runResult   *CheckResult
	runError    error
	fixResult   []Issue
	fixError    error
}

func (m *mockCheck) ID() string          { return m.id }
func (m *mockCheck) Name() string        { return m.name }
func (m *mockCheck) Description() string { return m.description }

func (m *mockCheck) Run(ctx context.Context, repoPath string) (*CheckResult, error) {
	if m.runError != nil {
		return nil, m.runError
	}
	return m.runResult, nil
}

func (m *mockCheck) Fix(ctx context.Context, repoPath string, issues []Issue) ([]Issue, error) {
	if m.fixError != nil {
		return nil, m.fixError
	}
	return m.fixResult, nil
}

func TestNewDoctor(t *testing.T) {
	t.Run("creates doctor with defaults", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		if d.repoRoot != "/test/repo" {
			t.Errorf("expected repoRoot %q, got %q", "/test/repo", d.repoRoot)
		}
		if d.options.Fix != false {
			t.Error("expected Fix to be false by default")
		}
		if len(d.checks) != 0 {
			t.Errorf("expected no checks, got %d", len(d.checks))
		}
	})

	t.Run("applies options", func(t *testing.T) {
		d := NewDoctor("/test/repo",
			WithFix(true),
			WithVerbose(true),
			WithJSON(true),
			WithSubmodulesOnly(true),
			WithChecks([]string{"url", "integrity"}),
		)
		if !d.options.Fix {
			t.Error("expected Fix to be true")
		}
		if !d.options.Verbose {
			t.Error("expected Verbose to be true")
		}
		if !d.options.JSON {
			t.Error("expected JSON to be true")
		}
		if !d.options.SubmodulesOnly {
			t.Error("expected SubmodulesOnly to be true")
		}
		if len(d.options.Checks) != 2 {
			t.Errorf("expected 2 checks, got %d", len(d.options.Checks))
		}
	})
}

func TestDoctorRegisterCheck(t *testing.T) {
	d := NewDoctor("/test/repo")
	check := &mockCheck{id: "test", name: "Test Check"}

	d.RegisterCheck(check)

	if len(d.Checks()) != 1 {
		t.Errorf("expected 1 check, got %d", len(d.Checks()))
	}
	if d.Checks()[0].ID() != "test" {
		t.Errorf("expected check ID 'test', got %q", d.Checks()[0].ID())
	}
}

func TestDoctorRun(t *testing.T) {
	t.Run("no checks returns success", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success with no checks")
		}
		if result.Passed != 0 || result.Failed != 0 || result.Warned != 0 {
			t.Error("expected all counters to be zero")
		}
	})

	t.Run("all checks pass", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{
			id:        "check1",
			runResult: &CheckResult{Passed: true},
		})
		d.RegisterCheck(&mockCheck{
			id:        "check2",
			runResult: &CheckResult{Passed: true},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success")
		}
		if result.Passed != 2 {
			t.Errorf("expected 2 passed, got %d", result.Passed)
		}
	})

	t.Run("check with errors", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{
			id: "failing",
			runResult: &CheckResult{
				Passed: false,
				Issues: []Issue{
					{Severity: SeverityError, CheckID: "failing", Description: "problem found"},
				},
			},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Success {
			t.Error("expected failure")
		}
		if result.Failed != 1 {
			t.Errorf("expected 1 failed, got %d", result.Failed)
		}
		if len(result.Issues) != 1 {
			t.Errorf("expected 1 issue, got %d", len(result.Issues))
		}
	})

	t.Run("check with warnings", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{
			id: "warning-check",
			runResult: &CheckResult{
				Passed: false,
				Issues: []Issue{
					{Severity: SeverityWarning, CheckID: "warning-check", Description: "minor issue"},
				},
			},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Success {
			t.Error("expected success (warnings don't fail)")
		}
		if result.Warned != 1 {
			t.Errorf("expected 1 warned, got %d", result.Warned)
		}
	})

	t.Run("graceful degradation on check error", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{
			id:       "error-check",
			runError: errors.New("check failed to run"),
		})
		d.RegisterCheck(&mockCheck{
			id:        "good-check",
			runResult: &CheckResult{Passed: true},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Should continue running other checks
		if result.Passed != 1 {
			t.Errorf("expected 1 passed, got %d", result.Passed)
		}
		if result.Failed != 1 {
			t.Errorf("expected 1 failed, got %d", result.Failed)
		}
		// Should record the error as an issue
		if len(result.Issues) != 1 {
			t.Errorf("expected 1 issue, got %d", len(result.Issues))
		}
	})

	t.Run("context cancellation before run", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{
			id:        "check1",
			runResult: &CheckResult{Passed: true},
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err := d.Run(ctx)
		if err == nil {
			t.Error("expected context error")
		}
	})

	t.Run("context cancellation during checks", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		ctx, cancel := context.WithCancel(context.Background())

		// First check will pass, then we cancel
		d.RegisterCheck(&mockCheck{
			id:        "check1",
			runResult: &CheckResult{Passed: true},
		})

		// This mock cancels context when run
		cancellingCheck := &mockCheck{
			id:        "canceller",
			runResult: &CheckResult{Passed: true},
		}
		d.RegisterCheck(cancellingCheck)
		d.RegisterCheck(&mockCheck{
			id:        "check3",
			runResult: &CheckResult{Passed: true},
		})

		// Modify first check to cancel after running
		firstCheck := d.checks[0].(*mockCheck)
		originalRun := firstCheck.runResult
		firstCheck.runResult = nil
		firstCheck.runError = nil

		// Replace with a function that cancels after first check
		d.checks[0] = &cancellingMockCheck{
			mockCheck: mockCheck{id: "check1", runResult: originalRun},
			cancelFn:  cancel,
		}

		_, err := d.Run(ctx)
		if err == nil {
			t.Error("expected context error")
		}
	})
}

// cancellingMockCheck cancels context after running.
type cancellingMockCheck struct {
	mockCheck
	cancelFn context.CancelFunc
}

func (c *cancellingMockCheck) Run(ctx context.Context, repoPath string) (*CheckResult, error) {
	result, err := c.mockCheck.Run(ctx, repoPath)
	c.cancelFn()
	return result, err
}

func TestDoctorFilterChecks(t *testing.T) {
	t.Run("filters to requested checks", func(t *testing.T) {
		d := NewDoctor("/test/repo", WithChecks([]string{"url"}))
		d.RegisterCheck(&mockCheck{id: "url", runResult: &CheckResult{Passed: true}})
		d.RegisterCheck(&mockCheck{id: "integrity", runResult: &CheckResult{Passed: true}})
		d.RegisterCheck(&mockCheck{id: "head", runResult: &CheckResult{Passed: true}})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Only url check should run
		if result.Passed != 1 {
			t.Errorf("expected 1 passed, got %d", result.Passed)
		}
	})

	t.Run("runs all when no filter", func(t *testing.T) {
		d := NewDoctor("/test/repo")
		d.RegisterCheck(&mockCheck{id: "url", runResult: &CheckResult{Passed: true}})
		d.RegisterCheck(&mockCheck{id: "integrity", runResult: &CheckResult{Passed: true}})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Passed != 2 {
			t.Errorf("expected 2 passed, got %d", result.Passed)
		}
	})
}

func TestDoctorFix(t *testing.T) {
	t.Run("fixes autoFixable issues", func(t *testing.T) {
		d := NewDoctor("/test/repo", WithFix(true))

		fixableIssue := Issue{
			Severity:    SeverityError,
			CheckID:     "url",
			Submodule:   "projects/test",
			Description: "URL mismatch",
			AutoFixable: true,
		}

		d.RegisterCheck(&mockCheck{
			id: "url",
			runResult: &CheckResult{
				Passed: false,
				Issues: []Issue{fixableIssue},
			},
			fixResult: []Issue{fixableIssue},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Fixed) != 1 {
			t.Errorf("expected 1 fixed, got %d", len(result.Fixed))
		}
		if !result.Success {
			t.Error("expected success after fix")
		}
	})

	t.Run("does not fix when Fix=false", func(t *testing.T) {
		d := NewDoctor("/test/repo", WithFix(false))

		fixableIssue := Issue{
			Severity:    SeverityError,
			CheckID:     "url",
			AutoFixable: true,
		}

		d.RegisterCheck(&mockCheck{
			id: "url",
			runResult: &CheckResult{
				Passed: false,
				Issues: []Issue{fixableIssue},
			},
			fixResult: []Issue{fixableIssue},
		})

		result, err := d.Run(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Fixed) != 0 {
			t.Errorf("expected 0 fixed, got %d", len(result.Fixed))
		}
	})
}

func TestSeverityString(t *testing.T) {
	tests := []struct {
		severity Severity
		expected string
	}{
		{SeverityInfo, "info"},
		{SeverityWarning, "warning"},
		{SeverityError, "error"},
		{Severity(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			if got := tt.severity.String(); got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestIssueHelpers(t *testing.T) {
	t.Run("IsError", func(t *testing.T) {
		issue := Issue{Severity: SeverityError}
		if !issue.IsError() {
			t.Error("expected IsError to be true")
		}
		issue.Severity = SeverityWarning
		if issue.IsError() {
			t.Error("expected IsError to be false")
		}
	})

	t.Run("IsWarning", func(t *testing.T) {
		issue := Issue{Severity: SeverityWarning}
		if !issue.IsWarning() {
			t.Error("expected IsWarning to be true")
		}
		issue.Severity = SeverityError
		if issue.IsWarning() {
			t.Error("expected IsWarning to be false")
		}
	})

	t.Run("CanFix", func(t *testing.T) {
		issue := Issue{AutoFixable: true, FixCommand: "git sync"}
		if !issue.CanFix() {
			t.Error("expected CanFix to be true")
		}
		issue.FixCommand = ""
		if issue.CanFix() {
			t.Error("expected CanFix to be false when no FixCommand")
		}
		issue.AutoFixable = false
		issue.FixCommand = "git sync"
		if issue.CanFix() {
			t.Error("expected CanFix to be false when not AutoFixable")
		}
	})
}
