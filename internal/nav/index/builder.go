package index

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/nav"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/worktree"
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
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	dir := filepath.Join(b.root, cat.Dir())
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, nil
	}

	targets, err := b.scanDirTargets(ctx, dir, cat)
	if err != nil || cat != nav.CategoryFestivals {
		return targets, err
	}
	return b.withNestedFestivalTargets(ctx, targets, cat)
}

func (b *Builder) withNestedFestivalTargets(ctx context.Context, buckets []Target, cat nav.Category) ([]Target, error) {
	all := make([]Target, 0, len(buckets))
	for _, bucket := range buckets {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		all = append(all, bucket)
		if !nav.IsFestivalStatusDir(bucket.Name) {
			continue
		}
		nested, err := b.scanDirTargets(ctx, bucket.Path, cat)
		if err != nil {
			continue
		}
		all = append(all, nested...)
	}
	return all, nil
}

func (b *Builder) scanDirTargets(ctx context.Context, dir string, cat nav.Category) ([]Target, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var targets []Target
	for _, entry := range entries {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		target, ok := b.entryTarget(dir, entry, cat)
		if ok {
			targets = append(targets, target)
		}
	}
	return targets, nil
}

func (b *Builder) entryTarget(dir string, entry os.DirEntry, cat nav.Category) (Target, bool) {
	if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "dungeon" {
		return Target{}, false
	}
	isDir := entry.IsDir()
	if !isDir && entry.Type()&os.ModeSymlink != 0 {
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		isDir = err == nil && info.IsDir()
	}
	if !isDir {
		return Target{}, false
	}
	target := Target{
		Name:     entry.Name(),
		Path:     filepath.Join(dir, entry.Name()),
		Category: cat,
	}
	if cat == nav.CategoryProjects {
		if projectCfg := b.findProjectConfig(entry.Name()); projectCfg != nil {
			target.Shortcuts = projectCfg.Shortcuts
		}
	}
	return target, true
}

// scanWorktrees enumerates git worktrees for every project in the campaign,
// using git as the source of truth. This finds every worktree regardless of
// where it lives on disk, not just those under the conventional
// projects/worktrees/<project>/ layout. Targets are named "project@name" where
// name is the worktree directory basename, preserving navigation ergonomics
// such as "cgo wt camp@feature".
//
// Projects are discovered with project.List (the same source of truth used by
// "camp worktrees list") rather than the campaign config, because the project
// set is derived from the projects/ checkout, not from campaign.yaml.
func (b *Builder) scanWorktrees(ctx context.Context) ([]Target, error) {
	projects, err := project.List(ctx, b.root)
	if err != nil {
		// Degrade gracefully: without a project list there are no worktree
		// targets to add, but the rest of the index is still valid.
		return nil, nil
	}

	var targets []Target
	seen := make(map[string]struct{})

	for _, proj := range projects {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		projectPath := project.ResolveProjectPath(b.root, proj)

		entries, err := worktree.NewGitWorktree(projectPath).List(ctx)
		if err != nil {
			// Not a git repo, a missing checkout, or a git failure: skip this
			// project rather than failing the whole index build.
			continue
		}

		for _, entry := range entries {
			target, ok := worktreeTarget(proj.Name, projectPath, entry)
			if !ok {
				continue
			}
			clean := filepath.Clean(entry.Path)
			if _, dup := seen[clean]; dup {
				continue
			}
			seen[clean] = struct{}{}
			targets = append(targets, target)
		}
	}

	return targets, nil
}

// worktreeTarget builds a navigation target for a linked worktree entry. It
// reports ok=false for entries that are not navigable parallel worktrees: the
// project's own main working tree, bare entries, git-internal paths, and hidden
// directories. The classification itself lives in worktree.IsLinkedWorktree so
// every enumerator (nav index, camp worktrees list, camp project worktree
// list) agrees on what counts as a linked worktree.
func worktreeTarget(projectName, projectPath string, entry worktree.GitWorktreeEntry) (Target, bool) {
	if !worktree.IsLinkedWorktree(projectPath, entry) {
		return Target{}, false
	}

	name := filepath.Base(filepath.Clean(entry.Path))

	return Target{
		Name:     projectName + "@" + name,
		Path:     entry.Path,
		Category: nav.CategoryWorktrees,
	}, true
}
