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

type snapshotStoreMock struct {
	saved map[string]*intleverage.Snapshot
}

func newSnapshotStoreMock() *snapshotStoreMock {
	return &snapshotStoreMock{saved: make(map[string]*intleverage.Snapshot)}
}

func (m *snapshotStoreMock) Save(_ context.Context, snapshot *intleverage.Snapshot) error {
	key := snapshot.Project + ":" + snapshot.Date
	m.saved[key] = snapshot
	return nil
}

func (m *snapshotStoreMock) Load(_ context.Context, project, date string) (*intleverage.Snapshot, error) {
	return m.saved[project+":"+date], nil
}

func (m *snapshotStoreMock) List(_ context.Context, project string) ([]string, error) {
	var dates []string
	prefix := project + ":"
	for key := range m.saved {
		if strings.HasPrefix(key, prefix) {
			dates = append(dates, strings.TrimPrefix(key, prefix))
		}
	}
	return dates, nil
}

func (m *snapshotStoreMock) LoadAll(ctx context.Context, project string) ([]*intleverage.Snapshot, error) {
	dates, _ := m.List(ctx, project)
	var snapshots []*intleverage.Snapshot
	for _, date := range dates {
		snapshots = append(snapshots, m.saved[project+":"+date])
	}
	return snapshots, nil
}

func (m *snapshotStoreMock) ListProjects(_ context.Context) ([]string, error) {
	projects := make(map[string]struct{})
	for key := range m.saved {
		parts := strings.SplitN(key, ":", 2)
		projects[parts[0]] = struct{}{}
	}
	var out []string
	for project := range projects {
		out = append(out, project)
	}
	return out, nil
}

func TestPersistCurrentSnapshots_SavesEachProjectAndReusesHeadLookup(t *testing.T) {
	store := newSnapshotStoreMock()
	commitDate := time.Date(2026, 4, 18, 10, 0, 0, 0, time.UTC)
	sampledAt := commitDate.Add(2 * time.Hour)

	inputs := []currentSnapshotInput{
		{
			project: intleverage.ResolvedProject{
				Name:   "obey-platform-monorepo",
				GitDir: "/tmp/obey-platform-monorepo",
				Authors: []intleverage.AuthorContribution{
					{Name: "A", Email: "a@example.com", Lines: 10},
				},
			},
			result: sampleResult(10, 12, 1000, 500),
			score:  &intleverage.LeverageScore{ProjectName: "obey-platform-monorepo"},
		},
		{
			project: intleverage.ResolvedProject{
				Name:   "obey-platform-monorepo@festui",
				GitDir: "/tmp/obey-platform-monorepo",
			},
			result: sampleResult(2, 3, 100, 50),
			score:  &intleverage.LeverageScore{ProjectName: "obey-platform-monorepo@festui"},
		},
	}

	headCalls := 0
	resolveHead := func(_ context.Context, gitDir string) (string, time.Time, error) {
		headCalls++
		if gitDir != "/tmp/obey-platform-monorepo" {
			t.Fatalf("unexpected git dir: %s", gitDir)
		}
		return "abc123", commitDate, nil
	}

	if err := persistCurrentSnapshots(context.Background(), store, inputs, sampledAt, resolveHead); err != nil {
		t.Fatalf("persistCurrentSnapshots failed: %v", err)
	}

	if headCalls != 1 {
		t.Fatalf("resolveHead called %d times, want 1", headCalls)
	}

	if len(store.saved) != 2 {
		t.Fatalf("saved %d snapshots, want 2", len(store.saved))
	}

	snap, err := store.Load(context.Background(), "obey-platform-monorepo", commitDate.Format("2006-01-02"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if snap == nil {
		t.Fatal("expected root project snapshot to be saved")
	}
	if snap.CommitHash != "abc123" {
		t.Fatalf("commit hash = %q, want abc123", snap.CommitHash)
	}
	if snap.SampledAt != sampledAt {
		t.Fatalf("sampled_at = %s, want %s", snap.SampledAt, sampledAt)
	}
	if snap.SCC == nil || snap.SCC.TotalCode == 0 {
		t.Fatal("expected SCC summary to be stored")
	}
}

func TestLeverageOutputTable_ShowsBackfillHintWhenRecentHistoryMissing(t *testing.T) {
	var buf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&buf)
	cmd.Flags().Bool("no-legend", false, "")

	score := &intleverage.LeverageScore{
		ProjectName:        "camp",
		EstimatedPeople:    10,
		EstimatedMonths:    12,
		EstimatedCost:      1000,
		ActualPeople:       1,
		ElapsedMonths:      1,
		ActualPersonMonths: 1,
		TotalFiles:         10,
		TotalLines:         600,
		TotalCode:          500,
		AuthorCount:        1,
		FullLeverage:       120,
		SimpleLeverage:     10,
	}
	cfg := &intleverage.LeverageConfig{
		ProjectStart: time.Date(2025, 4, 28, 0, 0, 0, 0, time.UTC),
	}

	if err := leverageOutputTable(cmd, score, []*intleverage.LeverageScore{score}, cfg, false, recentLeverage{
		needsBackfill: true,
	}, leverageOutputOpts{}); err != nil {
		t.Fatalf("leverageOutputTable failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Recent leverage history unavailable yet") {
		t.Fatalf("output missing backfill hint\nGot:\n%s", output)
	}
	if !strings.Contains(output, "camp leverage backfill") {
		t.Fatalf("output missing backfill command hint\nGot:\n%s", output)
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
