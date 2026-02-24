package leverage

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AuthorContribution represents a single author's LOC ownership in a project,
// derived from git blame output.
type AuthorContribution struct {
	Name       string  `json:"name"`
	Email      string  `json:"email"`
	Lines      int     `json:"lines"`                 // LOC owned (from git blame)
	Percentage float64 `json:"percentage"`            // lines / total_loc * 100
	WeightedPM float64 `json:"weighted_pm,omitempty"` // blame-weighted person-months
}

// LanguageSummary is a compact per-language summary stored in snapshots.
type LanguageSummary struct {
	Name       string `json:"name"`
	Files      int    `json:"files"`
	Code       int    `json:"code"`
	Complexity int    `json:"complexity"`
}

// SnapshotSCC holds the scc scan results captured at snapshot time.
type SnapshotSCC struct {
	EstimatedCost   float64           `json:"estimated_cost"`
	EstimatedMonths float64           `json:"estimated_schedule_months"`
	EstimatedPeople float64           `json:"estimated_people"`
	TotalFiles      int               `json:"total_files"`
	TotalLines      int               `json:"total_lines"`
	TotalCode       int               `json:"total_code"`
	TotalComments   int               `json:"total_comments"`
	TotalBlanks     int               `json:"total_blanks"`
	TotalComplexity int               `json:"total_complexity"`
	Languages       []LanguageSummary `json:"languages"`
}

// Snapshot is a point-in-time capture of a project's leverage metrics,
// including scc results, computed leverage score, and author attributions.
type Snapshot struct {
	Project    string               `json:"project"`
	CommitHash string               `json:"commit_hash"`
	CommitDate time.Time            `json:"commit_date"`
	SampledAt  time.Time            `json:"sampled_at"`
	Date       string               `json:"date"` // YYYY-MM-DD derived from CommitDate
	SCC        *SnapshotSCC         `json:"scc"`
	Leverage   *LeverageScore       `json:"leverage"`
	Authors    []AuthorContribution `json:"authors,omitempty"`
	TotalLines int                  `json:"total_lines"`
}

// NewSnapshot creates a Snapshot with Date derived from commitDate.
// TotalLines is taken from scc.TotalLines to avoid duplicate aggregation.
func NewSnapshot(project, commitHash string, commitDate, sampledAt time.Time, scc *SnapshotSCC, score *LeverageScore, authors []AuthorContribution) *Snapshot {
	var totalLines int
	if scc != nil {
		totalLines = scc.TotalLines
	}
	return &Snapshot{
		Project:    project,
		CommitHash: commitHash,
		CommitDate: commitDate,
		SampledAt:  sampledAt,
		Date:       commitDate.Format("2006-01-02"),
		SCC:        scc,
		Leverage:   score,
		Authors:    authors,
		TotalLines: totalLines,
	}
}

// SnapshotStorer persists and retrieves leverage snapshots.
type SnapshotStorer interface {
	// Save persists a snapshot. Overwrites any existing snapshot for the same
	// project and date.
	Save(ctx context.Context, snapshot *Snapshot) error

	// Load retrieves a single snapshot by project name and date string (YYYY-MM-DD).
	Load(ctx context.Context, project string, date string) (*Snapshot, error)

	// List returns sorted date strings (ascending) for all snapshots of a project.
	List(ctx context.Context, project string) ([]string, error)

	// LoadAll retrieves all snapshots for a project, sorted by date ascending.
	LoadAll(ctx context.Context, project string) ([]*Snapshot, error)

	// ListProjects returns the names of all projects that have snapshots.
	ListProjects(ctx context.Context) ([]string, error)
}

// FileSnapshotStore implements SnapshotStorer using flat JSON files.
// Storage layout: <baseDir>/<project>/<YYYY-MM-DD>.json
type FileSnapshotStore struct {
	baseDir string
}

// NewFileSnapshotStore creates a FileSnapshotStore rooted at baseDir.
func NewFileSnapshotStore(baseDir string) *FileSnapshotStore {
	return &FileSnapshotStore{baseDir: baseDir}
}

// DefaultSnapshotDir returns the default snapshot storage directory
// within a campaign root: .campaign/leverage/snapshots/
func DefaultSnapshotDir(campaignRoot string) string {
	return filepath.Join(campaignRoot, ".campaign", "leverage", "snapshots")
}

// validateProjectName ensures a project name is safe for filesystem paths.
func validateProjectName(name string) error {
	if name == "" || strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		return fmt.Errorf("invalid project name %q: must not be empty or contain path separators", name)
	}
	return nil
}

// Save persists a snapshot as a JSON file. Creates directories as needed.
// Overwrites any existing snapshot for the same project and date.
func (s *FileSnapshotStore) Save(ctx context.Context, snapshot *Snapshot) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	if err := validateProjectName(snapshot.Project); err != nil {
		return err
	}

	// Ensure Date is set (may already be set via NewSnapshot)
	if snapshot.Date == "" {
		snapshot.Date = snapshot.CommitDate.Format("2006-01-02")
	}

	dir := filepath.Join(s.baseDir, snapshot.Project)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating snapshot directory: %w", err)
	}

	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling snapshot: %w", err)
	}

	path := filepath.Join(dir, snapshot.Date+".json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing snapshot: %w", err)
	}

	return nil
}

// Load retrieves a single snapshot by project name and date string (YYYY-MM-DD).
func (s *FileSnapshotStore) Load(ctx context.Context, project string, date string) (*Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	path := filepath.Join(s.baseDir, project, date+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading snapshot %s/%s: %w", project, date, err)
	}

	var snap Snapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, fmt.Errorf("unmarshaling snapshot %s/%s: %w", project, date, err)
	}

	return &snap, nil
}

// List returns sorted date strings (ascending) for all snapshots of a project.
func (s *FileSnapshotStore) List(ctx context.Context, project string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	dir := filepath.Join(s.baseDir, project)
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading snapshot directory: %w", err)
	}

	var dates []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		dates = append(dates, strings.TrimSuffix(name, ".json"))
	}

	sort.Strings(dates)
	return dates, nil
}

// LoadAll retrieves all snapshots for a project, sorted by date ascending.
func (s *FileSnapshotStore) LoadAll(ctx context.Context, project string) ([]*Snapshot, error) {
	dates, err := s.List(ctx, project)
	if err != nil {
		return nil, err
	}

	snapshots := make([]*Snapshot, 0, len(dates))
	for _, date := range dates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		snap, err := s.Load(ctx, project, date)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snap)
	}

	return snapshots, nil
}

// ListProjects returns the names of all projects that have snapshots.
func (s *FileSnapshotStore) ListProjects(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading base directory: %w", err)
	}

	var projects []string
	for _, entry := range entries {
		if entry.IsDir() {
			projects = append(projects, entry.Name())
		}
	}

	sort.Strings(projects)
	return projects, nil
}
