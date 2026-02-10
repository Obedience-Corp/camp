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

	"github.com/obediencecorp/camp/internal/project"
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
		return nil, fmt.Errorf("reading leverage config: %w", err)
	}

	var cfg LeverageConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing leverage config: %w", err)
	}

	// Apply defaults for zero values
	if cfg.ActualPeople == 0 {
		cfg.ActualPeople = 1
	}
	if cfg.COCOMOProjectType == "" {
		cfg.COCOMOProjectType = COCOMOOrganic
	}

	return &cfg, nil
}

// SaveConfig writes cfg to path as indented JSON, creating directories as needed.
func SaveConfig(path string, cfg *LeverageConfig) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling leverage config: %w", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("writing leverage config: %w", err)
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
		return nil, fmt.Errorf("listing projects: %w", err)
	}

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

// defaultConfig returns a LeverageConfig with sensible defaults.
func defaultConfig() *LeverageConfig {
	return &LeverageConfig{
		ActualPeople:      1,
		COCOMOProjectType: COCOMOOrganic,
	}
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
		return time.Time{}, fmt.Errorf("git log in %s: %w", repoPath, err)
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
