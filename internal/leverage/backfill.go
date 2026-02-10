package leverage

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Backfiller reconstructs historical leverage data by checking out past commits
// via git worktrees and running scc + blame analysis at each sample point.
type Backfiller struct {
	runner     Runner
	store      SnapshotStorer
	workers    int
	onProgress func(project string, current, total int)
}

// NewBackfiller creates a Backfiller with the given dependencies.
func NewBackfiller(runner Runner, store SnapshotStorer, workers int) *Backfiller {
	if workers < 1 {
		workers = 1
	}
	return &Backfiller{
		runner:  runner,
		store:   store,
		workers: workers,
	}
}

// SetProgressCallback sets the function called after each sample is processed.
func (b *Backfiller) SetProgressCallback(fn func(project string, current, total int)) {
	b.onProgress = fn
}

// Run backfills leverage data for the given projects.
// Projects sharing the same GitDir are grouped and processed with shared worktrees.
func (b *Backfiller) Run(ctx context.Context, projects []ResolvedProject, cfg *LeverageConfig) error {
	groups := groupByGitDir(projects)
	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := b.backfillGroup(ctx, group, cfg); err != nil {
			return err
		}
	}
	return nil
}

// monorepoGroup represents a set of subprojects sharing the same git repository.
type monorepoGroup struct {
	GitDir   string
	Projects []ResolvedProject
}

// groupByGitDir groups projects by their GitDir, preserving insertion order.
func groupByGitDir(projects []ResolvedProject) []monorepoGroup {
	groupMap := make(map[string]*monorepoGroup)
	var order []string

	for _, p := range projects {
		if g, ok := groupMap[p.GitDir]; ok {
			g.Projects = append(g.Projects, p)
		} else {
			order = append(order, p.GitDir)
			groupMap[p.GitDir] = &monorepoGroup{
				GitDir:   p.GitDir,
				Projects: []ResolvedProject{p},
			}
		}
	}

	groups := make([]monorepoGroup, 0, len(order))
	for _, key := range order {
		groups = append(groups, *groupMap[key])
	}
	return groups
}

// backfillGroup processes a group of projects sharing the same GitDir.
func (b *Backfiller) backfillGroup(ctx context.Context, group monorepoGroup, cfg *LeverageConfig) error {
	samples, err := SampleWeeklyCommits(ctx, group.GitDir, cfg.ProjectStart)
	if err != nil {
		return fmt.Errorf("sampling commits for %s: %w", group.GitDir, err)
	}

	// Determine which samples need processing (incremental behavior).
	// A sample is skipped only if ALL projects in the group already have a snapshot for that date.
	type pendingSample struct {
		sample   CommitSample
		projects []ResolvedProject // projects needing backfill at this date
	}

	var pending []pendingSample
	for _, s := range samples {
		if err := ctx.Err(); err != nil {
			return err
		}

		dateStr := s.Date.Format("2006-01-02")
		var needWork []ResolvedProject
		for _, proj := range group.Projects {
			existing, err := b.store.List(ctx, proj.Name)
			if err != nil {
				return fmt.Errorf("listing snapshots for %s: %w", proj.Name, err)
			}
			if !containsDate(existing, dateStr) {
				needWork = append(needWork, proj)
			}
		}
		if len(needWork) > 0 {
			pending = append(pending, pendingSample{sample: s, projects: needWork})
		}
	}

	if len(pending) == 0 {
		return nil
	}

	// Process via worker pool using semaphore pattern.
	var (
		wg        sync.WaitGroup
		semaphore = make(chan struct{}, b.workers)
		errCh     = make(chan error, 1)
		processed int
		mu        sync.Mutex
	)

	for _, ps := range pending {
		if err := ctx.Err(); err != nil {
			break
		}

		select {
		case err := <-errCh:
			// Drain any error before continuing
			wg.Wait()
			return err
		default:
		}

		wg.Add(1)
		semaphore <- struct{}{} // acquire slot

		go func(ps pendingSample) {
			defer wg.Done()
			defer func() { <-semaphore }() // release slot

			if err := b.processSample(ctx, group.GitDir, ps.sample, ps.projects, cfg); err != nil {
				select {
				case errCh <- err:
				default:
				}
				return
			}

			mu.Lock()
			processed++
			current := processed
			mu.Unlock()

			if b.onProgress != nil {
				b.onProgress(group.Projects[0].Name, current, len(pending))
			}
		}(ps)
	}

	wg.Wait()

	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

// processSample creates a worktree, runs scc + blame for each project, persists snapshots, and cleans up.
func (b *Backfiller) processSample(ctx context.Context, gitDir string, sample CommitSample, projects []ResolvedProject, cfg *LeverageConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	// Create worktree in temp directory
	worktreeDir, cleanup, err := createWorktree(ctx, gitDir, sample.Hash)
	if err != nil {
		return err
	}
	defer cleanup()

	elapsed := ElapsedMonths(cfg.ProjectStart, sample.Date)

	for _, proj := range projects {
		if err := ctx.Err(); err != nil {
			return err
		}

		// Determine scc scan directory within worktree
		sccDir := worktreeDir
		if proj.InMonorepo {
			rel, err := filepath.Rel(proj.GitDir, proj.SCCDir)
			if err != nil {
				continue
			}
			sccDir = filepath.Join(worktreeDir, rel)
		}

		// Skip if subdirectory doesn't exist at this historical commit
		if _, err := os.Stat(sccDir); os.IsNotExist(err) {
			continue
		}

		// Run scc
		result, err := b.runner.Run(ctx, sccDir)
		if err != nil {
			continue // scc failure is non-fatal for individual samples
		}

		// Compute leverage score
		score := ComputeScore(result, cfg.ActualPeople, elapsed)
		score.ProjectName = proj.Name

		// Get author contributions via git blame
		authors, _ := GetAuthorLOC(ctx, sccDir)

		// Aggregate total lines
		var totalLines int
		for _, lang := range result.LanguageSummary {
			totalLines += lang.Lines
		}

		// Build and persist snapshot
		snapshot := &Snapshot{
			Project:    proj.Name,
			CommitHash: sample.Hash,
			CommitDate: sample.Date,
			SampledAt:  time.Now(),
			SCC:        SCCResultToSnapshotSCC(result),
			Leverage:   score,
			Authors:    authors,
			TotalLines: totalLines,
		}

		if err := b.store.Save(ctx, snapshot); err != nil {
			return fmt.Errorf("saving snapshot for %s: %w", proj.Name, err)
		}
	}

	return nil
}

// createWorktree creates a detached git worktree and returns the path and cleanup function.
// The cleanup function removes the worktree even if the parent context is cancelled.
func createWorktree(ctx context.Context, gitDir, commitHash string) (string, func(), error) {
	dir, err := os.MkdirTemp("", "camp-backfill-*")
	if err != nil {
		return "", nil, fmt.Errorf("creating temp dir: %w", err)
	}
	// Remove the temp dir first — git worktree add needs a non-existing path
	os.Remove(dir)

	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "worktree", "add", "--detach", dir, commitHash)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, fmt.Errorf("git worktree add: %s: %w", out, err)
	}

	cleanup := func() {
		// Use background context so cleanup succeeds even when parent is cancelled
		rmCmd := exec.CommandContext(context.Background(), "git", "-C", gitDir, "worktree", "remove", "--force", dir)
		rmCmd.Run()
		os.RemoveAll(dir) // belt and suspenders
	}

	return dir, cleanup, nil
}

// SCCResultToSnapshotSCC converts an SCCResult into a SnapshotSCC for persistence.
func SCCResultToSnapshotSCC(result *SCCResult) *SnapshotSCC {
	scc := &SnapshotSCC{
		EstimatedCost:   result.EstimatedCost,
		EstimatedMonths: result.EstimatedScheduleMonths,
		EstimatedPeople: result.EstimatedPeople,
	}

	for _, lang := range result.LanguageSummary {
		scc.TotalFiles += lang.Count
		scc.TotalLines += lang.Lines
		scc.TotalCode += lang.Code
		scc.TotalComments += lang.Comment
		scc.TotalBlanks += lang.Blank
		scc.TotalComplexity += lang.Complexity
		scc.Languages = append(scc.Languages, LanguageSummary{
			Name:       lang.Name,
			Files:      lang.Count,
			Code:       lang.Code,
			Complexity: lang.Complexity,
		})
	}

	return scc
}

// containsDate checks if a sorted date list contains the given date string.
func containsDate(dates []string, target string) bool {
	for _, d := range dates {
		if d == target {
			return true
		}
	}
	return false
}
