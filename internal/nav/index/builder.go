package index

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

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
			if os.IsNotExist(err) {
				// The bucket existed moments ago during the top-level scan
				// but is gone now: it lost a race with a concurrent
				// festival move, not a real scan failure. That bucket
				// simply has no nested festivals to index.
				continue
			}
			// A real scan error (permission, I/O) on a status bucket must
			// not produce a silently incomplete festival index: callers
			// resolving "camp go f <name>" against a partial index would
			// miss festivals that are actually there, and the result can be
			// cached for up to cacheMaxAge. Propagate the error so
			// scanCategory returns it too; Build's per-category loop then
			// omits the whole festivals category from the index (its
			// existing "some directories may not exist" degradation path)
			// instead of caching a status-bucket list that looks complete
			// but silently dropped one bucket's festivals.
			return nil, err
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
// Projects come from the projects/ checkout, not campaign.yaml. This uses the
// locations-only walk: the index needs a name and a path, not the remote URL
// and commit date List spends a subprocess apiece on. That also keeps every
// checkout of a shared remote, which List would dedup away along with its
// worktrees.
func (b *Builder) scanWorktrees(ctx context.Context) ([]Target, error) {
	projects, err := project.ListLocations(ctx, b.root)
	if err != nil {
		// Degrade gracefully: without a project list there are no worktree
		// targets to add, but the rest of the index is still valid.
		return nil, nil
	}

	perProject := b.projectWorktreeTargets(ctx, projects)
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var targets []Target
	seen := make(map[string]struct{})

	// Merge in project order so the fan-out does not reorder the index.
	for _, projectTargets := range perProject {
		for _, target := range projectTargets {
			clean := filepath.Clean(target.Path)
			if _, dup := seen[clean]; dup {
				continue
			}
			seen[clean] = struct{}{}
			targets = append(targets, target)
		}
	}

	return targets, nil
}

// projectWorktreeTargets returns one slice of targets per project, in order.
//
// Each project costs an independent "git worktree list" subprocess, and running
// dozens serially dominates the index build. A bounded pool makes the scan cost
// the slowest repo rather than their sum.
func (b *Builder) projectWorktreeTargets(ctx context.Context, projects []project.Project) [][]Target {
	results := make([][]Target, len(projects))
	if len(projects) == 0 {
		return results
	}

	// Subprocess waits, not CPU work, so keep a floor on low-core machines.
	limit := min(max(runtime.NumCPU(), 4), len(projects))

	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for i, proj := range projects {
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = worktreeTargetsFor(ctx, b.root, proj)
		}()
	}
	wg.Wait()

	return results
}

// worktreeTargetsFor lists one project's linked worktrees as navigation
// targets. A project that cannot be listed contributes none.
func worktreeTargetsFor(ctx context.Context, campaignRoot string, proj project.Project) []Target {
	projectPath := project.ResolveProjectPath(campaignRoot, proj)

	entries, err := worktree.NewGitWorktree(projectPath).List(ctx)
	if err != nil {
		// Not a git repo, a missing checkout, or a git failure: skip this
		// project rather than failing the whole index build.
		return nil
	}

	targets := make([]Target, 0, len(entries))
	for _, entry := range entries {
		target, ok := worktreeTarget(proj.Name, projectPath, entry)
		if !ok {
			continue
		}
		targets = append(targets, target)
	}

	return targets
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
