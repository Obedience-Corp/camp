package leverage

import (
	"strings"
	"testing"
)

func executeLeverageDir(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return executeLeverage(t, args...)
}

func TestLeverageDir_MutualExclusion(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	_, err := executeLeverageDir(t, "--dir", ".", "--project", "camp")
	if err == nil {
		t.Fatal("expected error for --dir + --project, got nil")
	}
	if !strings.Contains(err.Error(), "mutually exclusive") {
		t.Errorf("error = %q, want substring 'mutually exclusive'", err.Error())
	}
}

func TestLeverageDir_NonexistentDir(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	_, err := executeLeverageDir(t, "--dir", "/nonexistent/path/that/does/not/exist")
	if err == nil {
		t.Fatal("expected error for nonexistent dir, got nil")
	}
	if !strings.Contains(err.Error(), "directory not found") {
		t.Errorf("error = %q, want substring 'directory not found'", err.Error())
	}
}

// Directory reporting with real Git/scc runs in the containerized repository report suite.

func TestResolveTargetDir(t *testing.T) {
	tests := []struct {
		name    string
		dirFlag string
		args    []string
		wantDir bool
	}{
		{name: "no flag no args", wantDir: false},
		{name: "dir flag", dirFlag: ".", wantDir: true},
		{name: "positional arg", args: []string{"."}, wantDir: true},
		{name: "flag takes priority", dirFlag: "/tmp", args: []string{"."}, wantDir: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetFlagSet(Cmd.Flags())
			if tt.dirFlag != "" {
				_ = Cmd.Flags().Set("dir", tt.dirFlag)
			}

			result, err := resolveTargetDir(Cmd, tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.wantDir && result == "" {
				t.Error("expected non-empty dir, got empty")
			}
			if !tt.wantDir && result != "" {
				t.Errorf("expected empty dir, got %q", result)
			}
		})
	}
}
