package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/pflag"
)

// executeLeverageDir runs the leverage command with directory-mode args.
// Resets flag state like executeLeverage but shares the same rootCmd routing.
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

func TestLeverageDir_TableOutput(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
		},
	}

	// Use "." which resolves to the camp project directory
	output, err := executeLeverageDir(t, "--dir", ".")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// Should say "Directory Leverage", not "Campaign Leverage"
	if !strings.Contains(output, "Directory Leverage") {
		t.Errorf("output should contain 'Directory Leverage'\nGot:\n%s", output)
	}
	if strings.Contains(output, "Campaign Leverage") {
		t.Errorf("output should NOT contain 'Campaign Leverage' in directory mode\nGot:\n%s", output)
	}

	// Should NOT show the auto-detect config warning
	if strings.Contains(output, "camp leverage config") {
		t.Errorf("output should NOT suggest 'camp leverage config' in directory mode\nGot:\n%s", output)
	}

	// Standard table elements should still be present
	for _, want := range []string{"PROJECT", "FILES", "CODE", "LEVERAGE", "COCOMO Estimate:", "person-months"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestLeverageDir_JSONOutput(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
		},
	}

	output, err := executeLeverageDir(t, "--dir", ".", "--json")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// Should be valid JSON with campaign and projects keys
	if !strings.Contains(output, `"campaign"`) {
		t.Errorf("JSON output missing 'campaign' key\nGot:\n%s", output)
	}
	if !strings.Contains(output, `"projects"`) {
		t.Errorf("JSON output missing 'projects' key\nGot:\n%s", output)
	}
}

func TestLeverageDir_PositionalArg(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
		},
	}

	// Positional arg should work the same as --dir
	output, err := executeLeverageDir(t, ".")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	if !strings.Contains(output, "Directory Leverage") {
		t.Errorf("positional arg should trigger directory mode\nGot:\n%s", output)
	}
}

// TestLeverageDir_OutsideCampaign verifies that running --dir on a directory
// outside any campaign does NOT load campaign config from cwd.
// Regression test for PR #145 review finding.
func TestLeverageDir_OutsideCampaign(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	// Create a temp dir that is definitely outside any campaign
	tmpDir := t.TempDir()
	dirName := filepath.Base(tmpDir)

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			dirName: sampleResult(2.0, 6.0, 500000, 10000),
		},
	}

	output, err := executeLeverageDir(t, "--dir", tmpDir)
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// Should show "Directory Leverage", not "Campaign Leverage"
	if !strings.Contains(output, "Directory Leverage") {
		t.Errorf("output should contain 'Directory Leverage'\nGot:\n%s", output)
	}
	if strings.Contains(output, "Campaign Leverage") {
		t.Errorf("output should NOT contain 'Campaign Leverage'\nGot:\n%s", output)
	}
}

func TestResolveTargetDir(t *testing.T) {
	tests := []struct {
		name    string
		dirFlag string
		args    []string
		wantDir bool // true = non-empty result expected
	}{
		{name: "no flag no args", wantDir: false},
		{name: "dir flag", dirFlag: ".", wantDir: true},
		{name: "positional arg", args: []string{"."}, wantDir: true},
		{name: "flag takes priority", dirFlag: "/tmp", args: []string{"."}, wantDir: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset flags
			leverageCmd.Flags().VisitAll(func(f *pflag.Flag) {
				f.Changed = false
				f.Value.Set(f.DefValue)
			})

			if tt.dirFlag != "" {
				leverageCmd.Flags().Set("dir", tt.dirFlag)
			}

			result, err := resolveTargetDir(leverageCmd, tt.args)
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
