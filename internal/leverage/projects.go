package leverage

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"

	"github.com/obediencecorp/camp/internal/project"
)

// ResolvedProject is a project ready for leverage scoring.
// SCCDir is the directory scc scans; GitDir is the git repository root
// (they differ for monorepo subprojects).
type ResolvedProject struct {
	// Name identifies the project (map key from config, or directory name from fallback).
	Name string

	// SCCDir is the absolute path for scc scan.
	SCCDir string

	// GitDir is the absolute path for git operations (blame, log, worktree).
	GitDir string

	// InMonorepo marks the project as a subdirectory within a larger git repo.
	InMonorepo bool

	// ExcludeDirs lists subdirectory names that scc should skip when scanning.
	// Set on monorepo root entries to prevent double-counting submodule code.
	ExcludeDirs []string

	// AuthorCount is the number of distinct human authors detected from git.
	// Zero means not yet populated.
	AuthorCount int

	// ActualPersonMonths is the blame-weighted sum of each author's effort.
	// Computed by BlameWeightedPersonMonths. Zero means not yet populated.
	ActualPersonMonths float64

	// Authors holds enriched author contributions with blame-weighted PM.
	// Populated by PopulateProjectMetrics via BlameWeightedPersonMonths.
	Authors []AuthorContribution
}

// ResolveProjects resolves project entries into absolute paths for leverage scoring.
// When cfg.Projects is non-empty, entries are resolved from the config map.
// When cfg.Projects is empty, falls back to project.List() for backward compatibility.
func ResolveProjects(ctx context.Context, campaignRoot string, cfg *LeverageConfig) ([]ResolvedProject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if len(cfg.Projects) == 0 {
		return resolveFromProjectList(ctx, campaignRoot)
	}

	return resolveFromConfig(ctx, campaignRoot, cfg.Projects)
}

// resolveFromProjectList falls back to project.List() discovery.
// Monorepo subprojects get SCCDir pointing to the subproject and GitDir
// pointing to the monorepo root (where .git lives).
func resolveFromProjectList(ctx context.Context, campaignRoot string) ([]ResolvedProject, error) {
	projects, err := project.List(ctx, campaignRoot)
	if err != nil {
		return nil, fmt.Errorf("list projects: %w", err)
	}

	resolved := make([]ResolvedProject, 0, len(projects))
	for _, p := range projects {
		sccDir := filepath.Join(campaignRoot, p.Path)

		var gitDir string
		var inMonorepo bool
		if p.MonorepoRoot != "" {
			gitDir = filepath.Join(campaignRoot, p.MonorepoRoot)
			inMonorepo = true
		} else {
			gitDir = sccDir
		}

		resolved = append(resolved, ResolvedProject{
			Name:        p.Name,
			SCCDir:      sccDir,
			GitDir:      gitDir,
			InMonorepo:  inMonorepo,
			ExcludeDirs: p.ExcludeDirs,
		})
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})

	return resolved, nil
}

// resolveFromConfig resolves explicitly configured project entries.
func resolveFromConfig(ctx context.Context, campaignRoot string, projects map[string]ProjectEntry) ([]ResolvedProject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	resolved := make([]ResolvedProject, 0, len(projects))

	for name, entry := range projects {
		if !entry.Include {
			continue
		}

		if entry.Path == "" {
			return nil, fmt.Errorf("project %q: path is required", name)
		}

		sccDir := filepath.Join(campaignRoot, entry.Path)

		var gitDir string
		switch {
		case entry.InMonorepo && entry.MonorepoPath != "":
			gitDir = filepath.Join(campaignRoot, entry.MonorepoPath)
		case entry.GitRepo != "":
			gitDir = filepath.Join(campaignRoot, entry.GitRepo)
		default:
			gitDir = sccDir
		}

		resolved = append(resolved, ResolvedProject{
			Name:       name,
			SCCDir:     sccDir,
			GitDir:     gitDir,
			InMonorepo: entry.InMonorepo,
		})
	}

	sort.Slice(resolved, func(i, j int) bool {
		return resolved[i].Name < resolved[j].Name
	})

	return resolved, nil
}

// PopulateOneProject fills AuthorCount, ActualPersonMonths, and Authors
// for a single ResolvedProject from git data and blame attribution.
func PopulateOneProject(ctx context.Context, p *ResolvedProject) {
	count, err := CountAuthors(ctx, p.GitDir)
	if err == nil {
		p.AuthorCount = count
	}
	pm, authors, err := BlameWeightedPersonMonths(ctx, p.GitDir, p.SCCDir)
	if err == nil {
		p.ActualPersonMonths = pm
		p.Authors = authors
	}
}

// PopulateProjectMetrics fills AuthorCount, ActualPersonMonths, and Authors
// on each ResolvedProject from git data and blame attribution.
func PopulateProjectMetrics(ctx context.Context, resolved []ResolvedProject) {
	for i := range resolved {
		if err := ctx.Err(); err != nil {
			return
		}
		PopulateOneProject(ctx, &resolved[i])
	}
}

// FilterByName filters projects to only those matching name.
// If name is empty, all projects are returned unchanged.
// Returns an error if name is non-empty and no match is found.
func FilterByName(projects []ResolvedProject, name string) ([]ResolvedProject, error) {
	if name == "" {
		return projects, nil
	}
	var filtered []ResolvedProject
	for _, p := range projects {
		if p.Name == name {
			filtered = append(filtered, p)
		}
	}
	if len(filtered) == 0 {
		return nil, fmt.Errorf("project not found: %s", name)
	}
	return filtered, nil
}
