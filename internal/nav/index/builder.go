package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
)

// Builder builds the navigation index by scanning campaign directories.
type Builder struct {
	root     string
	projects []config.ProjectConfig
}

// NewBuilder creates a new index builder for a campaign root.
func NewBuilder(root string) *Builder {
	return &Builder{root: root}
}

// WithProjects sets project configs for the builder.
// This allows project shortcuts to be attached to targets.
func (b *Builder) WithProjects(projects []config.ProjectConfig) *Builder {
	b.projects = projects
	return b
}

// findProjectConfig finds project config by name.
func (b *Builder) findProjectConfig(name string) *config.ProjectConfig {
	for i := range b.projects {
		if b.projects[i].Name == name {
			return &b.projects[i]
		}
	}
	return nil
}

// Build scans the campaign and builds the navigation index.
func (b *Builder) Build(ctx context.Context) (*Index, error) {
	idx := NewIndex(b.root)

	// Scan each category
	categories := nav.ValidCategories()

	for _, cat := range categories {
		// Check context cancellation
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		// Worktrees use nested project@branch scanning, handled below
		if cat == nav.CategoryWorktrees {
			continue
		}

		// Dungeon contains archived/old work — exclude from fuzzy search
		if cat == nav.CategoryDungeon {
			continue
		}

		targets, err := b.scanCategory(ctx, cat)
		if err != nil {
			// Log but don't fail - some directories may not exist
			continue
		}
		idx.Targets = append(idx.Targets, targets...)
	}

	// Special handling for worktrees (nested structure)
	worktreeTargets, err := b.scanWorktrees(ctx)
	if err == nil {
		idx.Targets = append(idx.Targets, worktreeTargets...)
	}

	return idx, nil
}

// scanCategory scans a single category directory for targets.
func (b *Builder) scanCategory(ctx context.Context, cat nav.Category) ([]Target, error) {
	dir := filepath.Join(b.root, cat.Dir())

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check if directory exists
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil // Not an error, just skip
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	// List immediate children
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var targets []Target
	for _, entry := range entries {
		// Skip hidden entries
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}

		// Skip dungeon directories (archived/old work)
		if entry.Name() == "dungeon" {
			continue
		}

		isDir := entry.IsDir()
		if !isDir && entry.Type()&os.ModeSymlink != 0 {
			targetInfo, err := os.Stat(filepath.Join(dir, entry.Name()))
			isDir = err == nil && targetInfo.IsDir()
		}
		if !isDir {
			continue
		}

		target := Target{
			Name:     entry.Name(),
			Path:     filepath.Join(dir, entry.Name()),
			Category: cat,
		}

		// For projects, attach shortcuts from project config if available
		if cat == nav.CategoryProjects {
			if projectCfg := b.findProjectConfig(entry.Name()); projectCfg != nil {
				target.Shortcuts = projectCfg.Shortcuts
			}
		}

		targets = append(targets, target)
	}

	return targets, nil
}

// scanWorktrees scans the worktrees directory for git worktree targets.
// Worktrees are organized as: worktrees/<project>/<branch>
// Targets are named as "project@branch".
func (b *Builder) scanWorktrees(ctx context.Context) ([]Target, error) {
	worktreesDir := filepath.Join(b.root, nav.CategoryWorktrees.Dir())

	// Check context
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Check if worktrees directory exists
	info, err := os.Stat(worktreesDir)
	if os.IsNotExist(err) {
		return nil, nil // Not an error
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	// List projects
	projects, err := os.ReadDir(worktreesDir)
	if err != nil {
		return nil, err
	}

	var targets []Target
	for _, proj := range projects {
		// Check context
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		if !proj.IsDir() {
			continue
		}
		if strings.HasPrefix(proj.Name(), ".") {
			continue
		}
		if proj.Name() == "dungeon" {
			continue
		}

		// Each subdirectory is a worktree branch
		projDir := filepath.Join(worktreesDir, proj.Name())
		worktrees, err := os.ReadDir(projDir)
		if err != nil {
			continue
		}

		for _, wt := range worktrees {
			if !wt.IsDir() {
				continue
			}
			if strings.HasPrefix(wt.Name(), ".") {
				continue
			}

			targets = append(targets, Target{
				Name:     proj.Name() + "@" + wt.Name(),
				Path:     filepath.Join(projDir, wt.Name()),
				Category: nav.CategoryWorktrees,
			})
		}
	}

	return targets, nil
}
