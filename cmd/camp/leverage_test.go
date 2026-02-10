package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/pflag"
)

// mockRunner implements leverage.Runner for testing.
var _ leverage.Runner = (*mockRunner)(nil)

type mockRunner struct {
	// results maps directory base names to SCCResult responses.
	results map[string]*leverage.SCCResult
	err     error
}

func (m *mockRunner) Run(ctx context.Context, dir string) (*leverage.SCCResult, error) {
	if m.err != nil {
		return nil, m.err
	}

	// Match by last path component (project name)
	base := filepath.Base(dir)
	if result, ok := m.results[base]; ok {
		return result, nil
	}

	// Return empty result if project not in mock
	return &leverage.SCCResult{}, nil
}

// sampleResult returns a realistic SCCResult for testing.
func sampleResult(estimatedPeople, estimatedMonths, estimatedCost float64, code int) *leverage.SCCResult {
	return &leverage.SCCResult{
		LanguageSummary: []leverage.LanguageEntry{
			{Name: "Go", Lines: code + 200, Code: code, Comment: 100, Blank: 100},
		},
		EstimatedCost:           estimatedCost,
		EstimatedScheduleMonths: estimatedMonths,
		EstimatedPeople:         estimatedPeople,
	}
}

// executeLeverage runs the leverage command via rootCmd with proper routing.
// It resets flag state to avoid cross-test contamination.
func executeLeverage(t *testing.T, args ...string) (string, error) {
	t.Helper()

	// Reset Cobra flag state to avoid cross-test contamination.
	// Must reset both Changed and Value — Changed alone leaves stale values.
	leverageCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		f.Value.Set(f.DefValue)
	})
	leverageConfigCmd.Flags().VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		f.Value.Set(f.DefValue)
	})

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"leverage"}, args...))

	err := rootCmd.Execute()
	return buf.String(), err
}

func TestLeverageCommand_TableOutput(t *testing.T) {
	origRunner := sccRunner
	t.Cleanup(func() { sccRunner = origRunner })

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
			"fest": sampleResult(8.20, 15.50, 1500000, 45000),
		},
	}

	output, err := executeLeverage(t)
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	wantStrings := []string{
		"Campaign Leverage Score",
		"Effort:",
		"Team:",
		"PROJECT",
		"CODE",
		"EST PEOPLE",
		"EFFORT",
	}

	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestLeverageCommand_JSONOutput(t *testing.T) {
	origRunner := sccRunner
	t.Cleanup(func() { sccRunner = origRunner })

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
		},
	}

	output, err := executeLeverage(t, "--json")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	// Parse JSON to verify structure
	var result struct {
		Campaign *leverage.LeverageScore   `json:"campaign"`
		Projects []*leverage.LeverageScore `json:"projects"`
	}

	if err := json.Unmarshal([]byte(output), &result); err != nil {
		t.Fatalf("failed to parse JSON: %v\nGot:\n%s", err, output)
	}

	if result.Campaign == nil {
		t.Fatal("campaign score is nil in JSON output")
	}

	if result.Campaign.TotalCode == 0 {
		t.Error("campaign total_code is zero")
	}

	if len(result.Projects) == 0 {
		t.Error("no projects in JSON output")
	}
}

func TestLeverageCommand_ProjectFilter(t *testing.T) {
	origRunner := sccRunner
	t.Cleanup(func() { sccRunner = origRunner })

	sccRunner = &mockRunner{
		results: map[string]*leverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
			"fest": sampleResult(8.20, 15.50, 1500000, 45000),
		},
	}

	t.Run("valid project filter", func(t *testing.T) {
		output, err := executeLeverage(t, "--project", "camp")
		if err != nil {
			t.Fatalf("command failed: %v", err)
		}

		if !strings.Contains(output, "camp") {
			t.Errorf("output should contain filtered project 'camp'\nGot:\n%s", output)
		}
	})

	t.Run("invalid project filter returns error", func(t *testing.T) {
		_, err := executeLeverage(t, "--project", "nonexistent")
		if err == nil {
			t.Fatal("expected error for invalid project, got nil")
		}

		if !strings.Contains(err.Error(), "project not found") {
			t.Errorf("error = %q, want substring 'project not found'", err.Error())
		}
	})
}

func TestLeverageCommand_RunnerError(t *testing.T) {
	origRunner := sccRunner
	t.Cleanup(func() { sccRunner = origRunner })

	sccRunner = &mockRunner{
		err: fmt.Errorf("scc not found: install with 'brew install scc'"),
	}

	output, err := executeLeverage(t)

	// Command should succeed (project failures are warnings, not fatal).
	if err != nil {
		t.Fatalf("command should not error, got: %v", err)
	}

	if !strings.Contains(output, "Warning") {
		t.Errorf("output should contain warnings for skipped projects\nGot:\n%s", output)
	}
}

func TestLeverageConfigCommand_Display(t *testing.T) {
	output, err := executeLeverage(t, "config")
	if err != nil {
		t.Skipf("skipping: not in a campaign directory: %v", err)
	}

	wantStrings := []string{
		"Team Size:",
		"developer(s)",
		"COCOMO Type:",
		"Config path:",
	}

	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestLeverageConfigCommand_ValidationPeople(t *testing.T) {
	_, err := executeLeverage(t, "config", "--people", "0")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "people must be greater than 0") {
		t.Errorf("error = %q, want substring 'people must be greater than 0'", err.Error())
	}
}

func TestLeverageConfigCommand_ValidationDate(t *testing.T) {
	_, err := executeLeverage(t, "config", "--start", "2025-13-45")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid date format") {
		t.Errorf("error = %q, want substring 'invalid date format'", err.Error())
	}
}

func TestLeverageConfigCommand_ValidationCOCOMO(t *testing.T) {
	_, err := executeLeverage(t, "config", "--cocomo-type", "invalid")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid COCOMO type") {
		t.Errorf("error = %q, want substring 'invalid COCOMO type'", err.Error())
	}
}

func TestLeverageConfigCommand_SaveAndReload(t *testing.T) {
	tmpDir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0755); err != nil {
		t.Fatal(err)
	}

	configPath := leverage.DefaultConfigPath(tmpDir)
	cfg := &leverage.LeverageConfig{
		ActualPeople:      2,
		ProjectStart:      time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC),
		COCOMOProjectType: "organic",
	}

	if err := leverage.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}

	loaded, err := leverage.LoadConfig(configPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}

	if loaded.ActualPeople != 2 {
		t.Errorf("ActualPeople = %d, want 2", loaded.ActualPeople)
	}

	if loaded.COCOMOProjectType != "organic" {
		t.Errorf("COCOMOProjectType = %q, want %q", loaded.COCOMOProjectType, "organic")
	}
}
