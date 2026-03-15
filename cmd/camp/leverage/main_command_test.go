package leverage

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

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var _ intleverage.Runner = (*mockRunner)(nil)

type mockRunner struct {
	results map[string]*intleverage.SCCResult
	err     error
}

func (m *mockRunner) Run(ctx context.Context, dir string, excludeDirs []string) (*intleverage.SCCResult, error) {
	if m.err != nil {
		return nil, m.err
	}

	base := filepath.Base(dir)
	if result, ok := m.results[base]; ok {
		return result, nil
	}

	return &intleverage.SCCResult{}, nil
}

func sampleResult(estimatedPeople, estimatedMonths, estimatedCost float64, code int) *intleverage.SCCResult {
	return &intleverage.SCCResult{
		LanguageSummary: []intleverage.LanguageEntry{
			{Name: "Go", Lines: code + 200, Code: code, Comment: 100, Blank: 100, Count: 42},
		},
		EstimatedCost:           estimatedCost,
		EstimatedScheduleMonths: estimatedMonths,
		EstimatedPeople:         estimatedPeople,
	}
}

func resetFlagSet(flags *pflag.FlagSet) {
	flags.VisitAll(func(f *pflag.Flag) {
		f.Changed = false
		_ = f.Value.Set(f.DefValue)
	})
}

func newTestRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "camp",
		SilenceErrors: true,
		SilenceUsage:  true,
	}
	root.AddGroup(&cobra.Group{ID: "campaign", Title: "Campaign"})
	root.AddCommand(Cmd)
	return root
}

func executeLeverage(t *testing.T, args ...string) (string, error) {
	t.Helper()

	resetFlagSet(Cmd.Flags())
	resetFlagSet(leverageConfigCmd.Flags())
	resetFlagSet(leverageResetCmd.Flags())

	var buf bytes.Buffer
	root := newTestRootCmd()
	root.SetContext(context.Background())
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(append([]string{"leverage"}, args...))

	err := root.Execute()
	return buf.String(), err
}

func stubPopulateMetrics() func(ctx context.Context, campaignRoot string, resolved []intleverage.ResolvedProject, resolver *intleverage.AuthorResolver) {
	return func(ctx context.Context, _ string, resolved []intleverage.ResolvedProject, _ *intleverage.AuthorResolver) {
		for i := range resolved {
			resolved[i].AuthorCount = 1
			resolved[i].ActualPersonMonths = 1.0
			resolved[i].Authors = []intleverage.AuthorContribution{
				{Name: "Test Author", Email: "test@test.com", Lines: 100, Percentage: 100, WeightedPM: 1.0},
			}
		}
	}
}

func TestLeverageCommand_TableOutput(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*intleverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
			"fest": sampleResult(8.20, 15.50, 1500000, 45000),
		},
	}

	output, err := executeLeverage(t)
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	wantStrings := []string{
		"Campaign Leverage:",
		"COCOMO Estimate:",
		"person-months",
		"Actual Effort:",
		"Team Equivalent:",
		"PROJECT",
		"FILES",
		"CODE",
		"AUTHORS",
		"EST COST",
		"EST PM",
		"ACTUAL PM",
		"LEVERAGE",
	}

	for _, want := range wantStrings {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestLeverageCommand_JSONOutput(t *testing.T) {
	origRunner := sccRunner
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*intleverage.SCCResult{
			"camp": sampleResult(10.68, 18.72, 2251607, 65641),
		},
	}

	output, err := executeLeverage(t, "--json")
	if err != nil {
		t.Fatalf("command failed: %v", err)
	}

	var result struct {
		Campaign *intleverage.LeverageScore   `json:"campaign"`
		Projects []*intleverage.LeverageScore `json:"projects"`
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
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		results: map[string]*intleverage.SCCResult{
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
	origPopulate := populateMetrics
	t.Cleanup(func() { sccRunner = origRunner; populateMetrics = origPopulate })
	populateMetrics = stubPopulateMetrics()

	sccRunner = &mockRunner{
		err: fmt.Errorf("scc not found: install with 'brew install scc'"),
	}

	output, err := executeLeverage(t)
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

	for _, want := range []string{"Team Size:", "COCOMO Type:", "Config path:"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q\nGot:\n%s", want, output)
		}
	}
}

func TestLeverageConfigCommand_ValidationPeople(t *testing.T) {
	_, err := executeLeverage(t, "config", "--people", "-1")
	if err == nil {
		t.Fatal("expected error for negative people, got nil")
	}
	if !strings.Contains(err.Error(), "people must be") {
		t.Errorf("error = %q, want substring 'people must be'", err.Error())
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
	if err := os.MkdirAll(filepath.Join(tmpDir, ".campaign"), 0o755); err != nil {
		t.Fatal(err)
	}

	configPath := intleverage.DefaultConfigPath(tmpDir)
	cfg := &intleverage.LeverageConfig{
		ActualPeople:      2,
		ProjectStart:      time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC),
		COCOMOProjectType: "organic",
	}

	if err := intleverage.SaveConfig(configPath, cfg); err != nil {
		t.Fatalf("failed to save config: %v", err)
	}
	loaded, err := intleverage.LoadConfig(configPath)
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
