package complete

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/nav"
	"github.com/obediencecorp/camp/internal/nav/index"
)

// PathComplete completes partial path within a category.
// Uses prefix matching by default.
func PathComplete(ctx context.Context, cat nav.Category, partial string) ([]string, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return nil, err
	}

	q := index.NewQuery(idx)
	targets := q.ByCategory(cat)

	var candidates []string
	partialLower := strings.ToLower(partial)

	for _, t := range targets {
		nameLower := strings.ToLower(t.Name)

		// Prefix match first
		if strings.HasPrefix(nameLower, partialLower) {
			candidates = append(candidates, t.Name)
		}
	}

	return candidates, nil
}

// CompleteWorktree completes project@branch syntax for worktrees.
func CompleteWorktree(ctx context.Context, partial string) ([]string, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check for @ in partial
	if !strings.Contains(partial, "@") {
		// No @ yet - complete project names with @ suffix hint
		return completeProjectsWithAt(ctx, partial)
	}

	// Has @ - complete full worktree name
	return completeBranches(ctx, partial)
}

// completeProjectsWithAt lists worktree project directories from the filesystem.
// When partial is empty, returns all project directory names.
// When partial is non-empty, also includes matching project@branch entries from the index.
func completeProjectsWithAt(ctx context.Context, partial string) ([]string, error) {
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	worktreesDir := filepath.Join(jumpResult.Path, nav.CategoryWorktrees.Dir())

	entries, err := os.ReadDir(worktreesDir)
	if err != nil {
		return nil, nil
	}

	var candidates []string
	partialLower := strings.ToLower(partial)
	seen := make(map[string]bool)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if partial == "" || strings.HasPrefix(strings.ToLower(name), partialLower) {
			if !seen[name] {
				seen[name] = true
				candidates = append(candidates, name)
			}
		}
	}

	// Also include matching project@branch entries from index
	if partial != "" {
		idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
		if err == nil {
			q := index.NewQuery(idx)
			for _, t := range q.ByCategory(nav.CategoryWorktrees) {
				tLower := strings.ToLower(t.Name)
				if strings.HasPrefix(tLower, partialLower) && !seen[t.Name] {
					seen[t.Name] = true
					candidates = append(candidates, t.Name)
				}
			}
		}
	}

	return candidates, nil
}

// completeBranches completes full worktree names matching project@branch prefix.
func completeBranches(ctx context.Context, partial string) ([]string, error) {
	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	// Get or build index
	idx, err := index.GetOrBuild(ctx, jumpResult.Path, false)
	if err != nil {
		return nil, err
	}

	q := index.NewQuery(idx)
	targets := q.ByCategory(nav.CategoryWorktrees)

	var candidates []string
	partialLower := strings.ToLower(partial)

	for _, t := range targets {
		if strings.HasPrefix(strings.ToLower(t.Name), partialLower) {
			candidates = append(candidates, t.Name)
		}
	}

	return candidates, nil
}

// completeWorktreeRich returns rich completion candidates for worktrees.
// Hierarchical: shows project directories first, then project@branch on drill-down.
func completeWorktreeRich(ctx context.Context, campaignRoot, partial string) ([]index.CompletionCandidate, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if strings.Contains(partial, "@") {
		return completeBranchesRich(ctx, campaignRoot, partial)
	}

	worktreesDir := filepath.Join(campaignRoot, nav.CategoryWorktrees.Dir())
	return completeWorktreeProjects(ctx, campaignRoot, worktreesDir, partial)
}

// completeWorktreeProjects scans the worktrees directory for project subdirectories.
// Returns project directory names as candidates, plus matching project@branch from the index.
func completeWorktreeProjects(ctx context.Context, campaignRoot, worktreesDir, partial string) ([]index.CompletionCandidate, error) {
	entries, err := os.ReadDir(worktreesDir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var candidates []index.CompletionCandidate
	partialLower := strings.ToLower(partial)

	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		name := entry.Name()
		if partial == "" || strings.HasPrefix(strings.ToLower(name), partialLower) {
			relPath, _ := filepath.Rel(campaignRoot, filepath.Join(worktreesDir, name))
			candidates = append(candidates, index.CompletionCandidate{
				Name:     name,
				Path:     relPath,
				Category: string(nav.CategoryWorktrees),
			})
		}
	}

	// Also include matching project@branch entries from index
	if partial != "" {
		idx, err := index.GetOrBuild(ctx, campaignRoot, false)
		if err == nil {
			q := index.NewQuery(idx)
			for _, t := range q.ByCategory(nav.CategoryWorktrees) {
				tLower := strings.ToLower(t.Name)
				if strings.HasPrefix(tLower, partialLower) {
					candidates = append(candidates, index.CompletionCandidate{
						Name:     t.Name,
						Path:     t.RelativePath(campaignRoot),
						Category: string(nav.CategoryWorktrees),
					})
				}
			}
		}
	}

	return candidates, nil
}

// completeBranchesRich returns rich completion candidates for project@branch entries.
func completeBranchesRich(ctx context.Context, campaignRoot, partial string) ([]index.CompletionCandidate, error) {
	idx, err := index.GetOrBuild(ctx, campaignRoot, false)
	if err != nil {
		return nil, err
	}

	q := index.NewQuery(idx)
	targets := q.ByCategory(nav.CategoryWorktrees)

	var candidates []index.CompletionCandidate
	partialLower := strings.ToLower(partial)

	for _, t := range targets {
		if strings.HasPrefix(strings.ToLower(t.Name), partialLower) {
			candidates = append(candidates, index.CompletionCandidate{
				Name:     t.Name,
				Path:     t.RelativePath(campaignRoot),
				Category: string(nav.CategoryWorktrees),
			})
		}
	}

	return candidates, nil
}

// CompleteFestival completes festival paths including phases and sequences.
// Festivals have structure: festival-name/phase/sequence
func CompleteFestival(ctx context.Context, partial string) ([]string, error) {
	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get campaign root
	jumpResult, err := nav.DirectJump(ctx, nav.CategoryAll)
	if err != nil {
		return nil, err
	}

	festivalsDir := filepath.Join(jumpResult.Path, "festivals")

	// Check if festivals directory exists
	if _, err := os.Stat(festivalsDir); os.IsNotExist(err) {
		return nil, nil
	}

	parts := strings.Split(partial, "/")

	switch len(parts) {
	case 1:
		// Complete festival names
		return completeFestivalNames(ctx, festivalsDir, parts[0])
	case 2:
		// Complete phases
		return completeFestivalPhases(ctx, festivalsDir, parts[0], parts[1])
	case 3:
		// Complete sequences
		return completeFestivalSequences(ctx, festivalsDir, parts[0], parts[1], parts[2])
	default:
		return nil, nil
	}
}

// completeFestivalNames completes festival directory names.
func completeFestivalNames(ctx context.Context, festivalsDir, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check active and planned directories
	dirs := []string{
		filepath.Join(festivalsDir, "active"),
		filepath.Join(festivalsDir, "planned"),
	}

	var candidates []string
	partialLower := strings.ToLower(partial)
	seen := make(map[string]bool)

	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if !seen[name] && strings.HasPrefix(strings.ToLower(name), partialLower) {
				seen[name] = true
				candidates = append(candidates, name+"/")
			}
		}
	}

	return candidates, nil
}

// completeFestivalPhases completes phase directories within a festival.
func completeFestivalPhases(ctx context.Context, festivalsDir, festival, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Try active then planned
	festivalDirs := []string{
		filepath.Join(festivalsDir, "active", festival),
		filepath.Join(festivalsDir, "planned", festival),
	}

	var candidates []string
	partialLower := strings.ToLower(partial)

	for _, festivalDir := range festivalDirs {
		entries, err := os.ReadDir(festivalDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(name), partialLower) {
				candidates = append(candidates, festival+"/"+name+"/")
			}
		}
	}

	return candidates, nil
}

// completeFestivalSequences completes sequence directories within a phase.
func completeFestivalSequences(ctx context.Context, festivalsDir, festival, phase, partial string) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Try active then planned
	phaseDirs := []string{
		filepath.Join(festivalsDir, "active", festival, phase),
		filepath.Join(festivalsDir, "planned", festival, phase),
	}

	var candidates []string
	partialLower := strings.ToLower(partial)

	for _, phaseDir := range phaseDirs {
		entries, err := os.ReadDir(phaseDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if strings.HasPrefix(strings.ToLower(name), partialLower) {
				candidates = append(candidates, festival+"/"+phase+"/"+name)
			}
		}
	}

	return candidates, nil
}
