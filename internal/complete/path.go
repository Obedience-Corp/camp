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

// completeProjectsWithAt completes project names and adds @ suffix.
// Also matches branch names directly so "lever<TAB>" finds "camp@leverage-score".
func completeProjectsWithAt(ctx context.Context, partial string) ([]string, error) {
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

	seenProjects := make(map[string]bool)
	seenFull := make(map[string]bool)
	var candidates []string
	partialLower := strings.ToLower(partial)

	for _, t := range targets {
		// Parse "project@branch" format
		atIdx := strings.Index(t.Name, "@")
		if atIdx <= 0 {
			continue
		}

		project := t.Name[:atIdx]
		branch := t.Name[atIdx+1:]

		// Match project prefix → suggest "project@" for drilling down
		if !seenProjects[project] && strings.HasPrefix(strings.ToLower(project), partialLower) {
			seenProjects[project] = true
			candidates = append(candidates, project+"@")
		}

		// Match branch prefix → suggest full "project@branch"
		if !seenFull[t.Name] && strings.HasPrefix(strings.ToLower(branch), partialLower) {
			seenFull[t.Name] = true
			candidates = append(candidates, t.Name)
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
