package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/project"
)

func TestDefaultConfigPath(t *testing.T) {
	got := DefaultConfigPath("/home/user/campaign")
	want := filepath.Join("/home/user/campaign", ".campaign", "leverage", "config.json")
	if got != want {
		t.Errorf("DefaultConfigPath: want %s, got %s", want, got)
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	cfg, err := LoadConfig("/nonexistent/path/config.json")
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if cfg.ActualPeople != 0 {
		t.Errorf("ActualPeople default: want 0 (auto-detect), got %d", cfg.ActualPeople)
	}
	if cfg.COCOMOProjectType != COCOMOOrganic {
		t.Errorf("COCOMOProjectType default: want %s, got %s", COCOMOOrganic, cfg.COCOMOProjectType)
	}
}

func TestSaveConfig_LoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".campaign", "leverage", "config.json")

	original := &LeverageConfig{
		ActualPeople:      3,
		ProjectStart:      time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		COCOMOProjectType: COCOMOOrganic,
		AvgWage:           70000,
	}

	if err := SaveConfig(path, original); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config file not created: %v", err)
	}

	loaded, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if loaded.ActualPeople != original.ActualPeople {
		t.Errorf("ActualPeople: want %d, got %d", original.ActualPeople, loaded.ActualPeople)
	}
	if !loaded.ProjectStart.Equal(original.ProjectStart) {
		t.Errorf("ProjectStart: want %v, got %v", original.ProjectStart, loaded.ProjectStart)
	}
	if loaded.AvgWage != original.AvgWage {
		t.Errorf("AvgWage: want %f, got %f", original.AvgWage, loaded.AvgWage)
	}
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte("not json"), 0644)

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestLoadConfig_ZeroValuesGetDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Write config with zero values for ActualPeople and no COCOMOProjectType
	os.WriteFile(path, []byte(`{"actual_people": 0}`), 0644)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	// ActualPeople 0 is now valid (auto-detect from git), should stay 0
	if cfg.ActualPeople != 0 {
		t.Errorf("ActualPeople: want 0 (auto-detect), got %d", cfg.ActualPeople)
	}
	if cfg.COCOMOProjectType != COCOMOOrganic {
		t.Errorf("COCOMOProjectType: want %s (default), got %s", COCOMOOrganic, cfg.COCOMOProjectType)
	}
}

func TestEarliestCommitDate(t *testing.T) {
	// Create a temp git repo with a known commit date
	dir := t.TempDir()

	cmds := [][]string{
		{"git", "-C", dir, "init"},
		{"git", "-C", dir, "config", "user.email", "test@test.com"},
		{"git", "-C", dir, "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		if err := exec.Command(args[0], args[1:]...).Run(); err != nil {
			t.Fatalf("setup %v: %v", args, err)
		}
	}

	// Create a file and commit with a known date
	os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0644)
	cmd := exec.Command("git", "-C", dir, "add", ".")
	cmd.Run()
	cmd = exec.Command("git", "-C", dir, "commit", "-m", "initial")
	cmd.Env = append(os.Environ(),
		"GIT_COMMITTER_DATE=2025-03-15T10:00:00+00:00",
		"GIT_AUTHOR_DATE=2025-03-15T10:00:00+00:00",
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	date, err := earliestCommitDate(context.Background(), dir)
	if err != nil {
		t.Fatalf("earliestCommitDate: %v", err)
	}

	expected := time.Date(2025, 3, 15, 10, 0, 0, 0, time.UTC)
	if !date.Equal(expected) {
		t.Errorf("date: want %v, got %v", expected, date)
	}
}

func TestEarliestCommitDate_NotARepo(t *testing.T) {
	dir := t.TempDir()
	_, err := earliestCommitDate(context.Background(), dir)
	if err == nil {
		t.Fatal("expected error for non-git directory")
	}
}

func TestEarliestCommitDate_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := earliestCommitDate(ctx, ".")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestAutoDetectConfig(t *testing.T) {
	// Test with the actual campaign root
	campaignRoot := "/Users/lancerogers/Dev/AI/obey-campaign"
	if _, err := os.Stat(filepath.Join(campaignRoot, "projects")); err != nil {
		t.Skip("campaign root not available")
	}

	ctx := context.Background()
	cfg, err := AutoDetectConfig(ctx, campaignRoot)
	if err != nil {
		t.Fatalf("AutoDetectConfig: %v", err)
	}

	if cfg.ActualPeople != 0 {
		t.Errorf("ActualPeople: want 0 (auto-detect), got %d", cfg.ActualPeople)
	}
	if cfg.ProjectStart.IsZero() {
		t.Error("ProjectStart should not be zero")
	}
	if cfg.COCOMOProjectType != COCOMOOrganic {
		t.Errorf("COCOMOProjectType: want %s, got %s", COCOMOOrganic, cfg.COCOMOProjectType)
	}
}

func TestAutoDetectConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := AutoDetectConfig(ctx, "/Users/lancerogers/Dev/AI/obey-campaign")
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestPopulateProjectsFromDiscoveredPrunesDuplicateRepoURLs(t *testing.T) {
	const sharedURL = "git@github.com:test/shared.git"

	cfg := &LeverageConfig{
		Projects: map[string]ProjectEntry{
			"mono-a@shared": {
				Path:         "projects/mono-a/shared",
				Include:      false,
				InMonorepo:   true,
				MonorepoPath: "projects/mono-a",
			},
			"mono-b@shared": {
				Path:         "projects/mono-b/shared",
				Include:      true,
				InMonorepo:   true,
				MonorepoPath: "projects/mono-b",
			},
			"gone": {
				Path:    "projects/gone",
				Include: true,
			},
		},
	}
	projects := []project.Project{
		{Name: "mono-a", Path: "projects/mono-a", URL: "git@github.com:test/mono-a.git"},
		{Name: "mono-a@shared", Path: "projects/mono-a/shared", URL: sharedURL, MonorepoRoot: "projects/mono-a"},
		{Name: "mono-b", Path: "projects/mono-b", URL: "git@github.com:test/mono-b.git"},
		{Name: "mono-b@shared", Path: "projects/mono-b/shared", URL: sharedURL, MonorepoRoot: "projects/mono-b"},
	}

	populateProjectsFromDiscovered(cfg, projects)

	if _, ok := cfg.Projects["mono-b@shared"]; ok {
		t.Fatalf("duplicate submodule entry mono-b@shared should be pruned from leverage config")
	}
	if _, ok := cfg.Projects["gone"]; ok {
		t.Fatalf("stale project entry should be pruned from leverage config")
	}
	if got := cfg.Projects["mono-a@shared"]; got.Include {
		t.Fatalf("existing Include=false should be preserved for canonical duplicate entry")
	}
	for _, want := range []string{"mono-a", "mono-a@shared", "mono-b"} {
		if _, ok := cfg.Projects[want]; !ok {
			t.Fatalf("expected project %q to be present after population", want)
		}
	}
}

func TestSaveConfig_ReadOnlyDir(t *testing.T) {
	dir := t.TempDir()
	// Create a read-only directory so MkdirAll inside it fails
	roDir := filepath.Join(dir, "readonly")
	os.MkdirAll(roDir, 0555)
	path := filepath.Join(roDir, "subdir", "config.json")

	cfg := &LeverageConfig{ActualPeople: 1}
	err := SaveConfig(path, cfg)
	if err == nil {
		t.Fatal("expected error writing to read-only directory")
	}
}

func TestLoadConfig_UnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	os.WriteFile(path, []byte(`{"actual_people": 1}`), 0644)
	os.Chmod(path, 0000)
	t.Cleanup(func() { os.Chmod(path, 0644) })

	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for unreadable file")
	}
}
