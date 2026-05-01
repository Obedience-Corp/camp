package leverage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/project"
)

// DefaultConfigPath returns the path to the leverage config file
// within the given campaign root.
func DefaultConfigPath(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "leverage", "config.json")
}

// LoadConfig reads the leverage config from path.
// If the file does not exist, it returns a default config (not an error).
func LoadConfig(path string) (*LeverageConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultConfig(), nil
		}
		return nil, camperrors.Wrap(err, "reading leverage config")
	}

	var cfg LeverageConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, camperrors.Wrap(err, "parsing leverage config")
	}

	// Apply defaults for zero values.
	// ActualPeople == 0 is valid (means auto-detect from git).
	if cfg.COCOMOProjectType == "" {
		cfg.COCOMOProjectType = COCOMOOrganic
	}

	return &cfg, nil
}

// SaveConfig writes cfg to path as indented JSON, creating directories as needed.
func SaveConfig(path string, cfg *LeverageConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return camperrors.Wrap(err, "creating config directory")
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "marshaling leverage config")
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return camperrors.Wrap(err, "writing leverage config")
	}
	return nil
}

// AutoDetectConfig discovers projects via project.List() and finds the
// earliest git commit date across all projects to use as ProjectStart.
// Returns a config with ActualPeople=1 and all discovered projects.
func AutoDetectConfig(ctx context.Context, campaignRoot string) (*LeverageConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	projects, err := project.List(ctx, campaignRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "listing projects")
	}
	projects = deduplicateProjectsForLeverage(projects)

	cfg := defaultConfig()

	var earliest time.Time
	for _, proj := range projects {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// proj.Path is relative (e.g., "projects/camp"), make absolute
		absPath := filepath.Join(campaignRoot, proj.Path)

		date, err := earliestCommitDate(ctx, absPath)
		if err != nil {
			continue // skip projects where git fails
		}

		if earliest.IsZero() || date.Before(earliest) {
			earliest = date
		}
	}

	if !earliest.IsZero() {
		cfg.ProjectStart = earliest
	}

	return cfg, nil
}

// PopulateProjects fills cfg.Projects from project.List() auto-discovery.
// Existing entries (and their Include state) are preserved. Stale entries
// for projects that no longer exist on disk are removed.
func PopulateProjects(ctx context.Context, campaignRoot string, cfg *LeverageConfig) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	projects, err := project.List(ctx, campaignRoot)
	if err != nil {
		return camperrors.Wrap(err, "listing projects")
	}

	populateProjectsFromDiscovered(cfg, projects)
	return nil
}

func populateProjectsFromDiscovered(cfg *LeverageConfig, projects []project.Project) {
	projects = deduplicateProjectsForLeverage(projects)

	if cfg.Projects == nil {
		cfg.Projects = make(map[string]ProjectEntry, len(projects))
	}

	// Track discovered project names to prune stale entries.
	discovered := make(map[string]bool, len(projects))

	for _, p := range projects {
		discovered[p.Name] = true

		if _, exists := cfg.Projects[p.Name]; exists {
			continue
		}

		entry := ProjectEntry{
			Path:    p.Path,
			Include: true,
		}
		if p.MonorepoRoot != "" {
			entry.InMonorepo = true
			entry.MonorepoPath = p.MonorepoRoot
		}
		cfg.Projects[p.Name] = entry
	}

	// Remove stale entries for projects no longer on disk.
	for name := range cfg.Projects {
		if !discovered[name] {
			delete(cfg.Projects, name)
		}
	}
}

// defaultConfig returns a LeverageConfig with sensible defaults.
// ActualPeople defaults to 0 (auto-detect from git).
func defaultConfig() *LeverageConfig {
	return &LeverageConfig{
		ActualPeople:      0,
		COCOMOProjectType: COCOMOOrganic,
	}
}

// GitDateRange returns the first and last commit dates for a git repository.
// First is the earliest root commit; last is the most recent commit on any branch.
func GitDateRange(ctx context.Context, repoPath string) (first, last time.Time, err error) {
	first, err = earliestCommitDate(ctx, repoPath)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	last, err = latestCommitDate(ctx, repoPath)
	if err != nil {
		return time.Time{}, time.Time{}, err
	}
	return first, last, nil
}

// latestCommitDate returns the date of the most recent commit in a git repo.
func latestCommitDate(ctx context.Context, repoPath string) (time.Time, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"log", "--all", "-1", "--format=%cI")

	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, camperrors.Wrapf(err, "git log in %s", repoPath)
	}

	dateStr := strings.TrimSpace(string(output))
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("no commits in %s", repoPath)
	}

	t, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		return time.Time{}, camperrors.Wrapf(err, "parsing commit date in %s", repoPath)
	}

	return t, nil
}

// earliestCommitDate returns the date of the first commit in a git repo.
// Uses --max-parents=0 to find root commits (initial commits with no parents)
// across all branches. This is correct unlike --reverse --max-count=1 where
// git applies --max-count before --reverse, returning the latest commit.
func earliestCommitDate(ctx context.Context, repoPath string) (time.Time, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"log", "--all", "--max-parents=0", "--format=%cI")

	output, err := cmd.Output()
	if err != nil {
		return time.Time{}, camperrors.Wrapf(err, "git log in %s", repoPath)
	}

	dateStr := strings.TrimSpace(string(output))
	if dateStr == "" {
		return time.Time{}, fmt.Errorf("no commits in %s", repoPath)
	}

	// There may be multiple root commits (merged unrelated histories).
	// Find the earliest one.
	var earliest time.Time
	for _, line := range strings.Split(dateStr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, line)
		if err != nil {
			continue
		}
		if earliest.IsZero() || t.Before(earliest) {
			earliest = t
		}
	}

	if earliest.IsZero() {
		return time.Time{}, fmt.Errorf("no valid commit dates in %s", repoPath)
	}

	return earliest, nil
}
