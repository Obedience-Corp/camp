package leverage

import (
	"context"
	"errors"
	"testing"
)

// MockRunner implements Runner for testing without the scc binary.
type MockRunner struct {
	Result *SCCResult
	Err    error
}

func (m *MockRunner) Run(ctx context.Context, dir string) (*SCCResult, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Result, nil
}

func TestMockRunner_ImplementsRunner(t *testing.T) {
	// Compile-time check that MockRunner satisfies Runner.
	var _ Runner = (*MockRunner)(nil)
}

func TestMockRunner_Success(t *testing.T) {
	expected := &SCCResult{
		EstimatedPeople:         10.0,
		EstimatedScheduleMonths: 5.0,
		EstimatedCost:           100000,
		LanguageSummary: []LanguageEntry{
			{Name: "Go", Lines: 1000, Code: 800},
		},
	}

	mock := &MockRunner{Result: expected}
	result, err := mock.Run(context.Background(), "/any/path")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.EstimatedPeople != 10.0 {
		t.Errorf("EstimatedPeople: want 10.0, got %f", result.EstimatedPeople)
	}
}

func TestMockRunner_Error(t *testing.T) {
	mock := &MockRunner{Err: errors.New("scc failed")}
	result, err := mock.Run(context.Background(), "/any/path")
	if err == nil {
		t.Fatal("expected error")
	}
	if result != nil {
		t.Error("expected nil result on error")
	}
}

func TestNewSCCRunner(t *testing.T) {
	// scc is installed on this machine, so this should succeed.
	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Skipf("scc not installed: %v", err)
	}
	if runner == nil {
		t.Fatal("expected non-nil runner")
	}
}

func TestSCCRunner_Run(t *testing.T) {
	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Skipf("scc not installed: %v", err)
	}

	// Run against the camp project itself
	ctx := context.Background()
	result, err := runner.Run(ctx, ".")
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}

	if result.EstimatedPeople <= 0 {
		t.Error("expected positive EstimatedPeople")
	}
	if result.EstimatedScheduleMonths <= 0 {
		t.Error("expected positive EstimatedScheduleMonths")
	}
	if len(result.LanguageSummary) == 0 {
		t.Error("expected non-empty LanguageSummary")
	}
}

func TestSCCRunner_Run_ContextCancelled(t *testing.T) {
	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Skipf("scc not installed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err = runner.Run(ctx, ".")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestSCCRunner_Run_InvalidDir(t *testing.T) {
	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Skipf("scc not installed: %v", err)
	}

	// Run against a nonexistent directory — scc should fail with exit error
	_, err = runner.Run(context.Background(), "/nonexistent/dir/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for nonexistent directory")
	}
}

func TestSCCRunner_Run_EmptyDir(t *testing.T) {
	runner, err := NewSCCRunner(COCOMOOrganic)
	if err != nil {
		t.Skipf("scc not installed: %v", err)
	}

	// scc on an empty dir produces valid json2 with zero results
	dir := t.TempDir()
	result, err := runner.Run(context.Background(), dir)
	if err != nil {
		// Some scc versions may error on empty dirs, which is fine
		return
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
}
