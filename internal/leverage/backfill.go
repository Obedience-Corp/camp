package leverage

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Backfiller reconstructs historical leverage data by checking out past commits
// via git worktrees and running scc + blame analysis at each sample point.
type Backfiller struct {
	runner     Runner
	store      SnapshotStorer
	workers    int
	onProgress func(project string, current, total int)
	onWarning  func(project, sample string, err error)
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

// SetWarningCallback sets the function called when a non-fatal error is skipped
// (e.g., scc failure or blame error on an individual sample).
func (b *Backfiller) SetWarningCallback(fn func(project, sample string, err error)) {
	b.onWarning = fn
}

// warn emits a non-fatal warning if a callback is registered.
func (b *Backfiller) warn(project, sample string, err error) {
	if b.onWarning != nil {
		b.onWarning(project, sample, err)
	}
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

// pendingSample pairs a commit sample with the projects that still need a snapshot at that date.
type pendingSample struct {
	sample   CommitSample
	projects []ResolvedProject
}

// buildPendingSamples filters samples to only those needing work for at least one project.
// A sample is skipped if ALL projects already have a snapshot for that date.
func (b *Backfiller) buildPendingSamples(ctx context.Context, samples []CommitSample, projects []ResolvedProject) ([]pendingSample, error) {
	var pending []pendingSample
	for _, s := range samples {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		dateStr := s.Date.Format("2006-01-02")
		var needWork []ResolvedProject
		for _, proj := range projects {
			existing, err := b.store.List(ctx, proj.Name)
			if err != nil {
				return nil, camperrors.Wrapf(err, "listing snapshots for %s", proj.Name)
			}
			if !containsDate(existing, dateStr) {
				needWork = append(needWork, proj)
			}
		}
		if len(needWork) > 0 {
			pending = append(pending, pendingSample{sample: s, projects: needWork})
		}
	}
	return pending, nil
}

// backfillGroup processes a group of projects sharing the same GitDir.
func (b *Backfiller) backfillGroup(ctx context.Context, group monorepoGroup, cfg *LeverageConfig) error {
	samples, err := SampleWeeklyCommits(ctx, group.GitDir, cfg.ProjectStart)
	if err != nil {
		return camperrors.Wrapf(err, "sampling commits for %s", group.GitDir)
	}

	pending, err := b.buildPendingSamples(ctx, samples, group.Projects)
	if err != nil {
		return err
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
		dateStr := sample.Date.Format("2006-01-02")
		sccDir := worktreeDir
		if proj.InMonorepo {
			rel, err := filepath.Rel(proj.GitDir, proj.SCCDir)
			if err != nil {
				b.warn(proj.Name, dateStr, camperrors.Wrap(err, "resolving monorepo path"))
				continue
			}
			sccDir = filepath.Join(worktreeDir, rel)
		}

		// Skip if subdirectory doesn't exist at this historical commit
		if _, err := os.Stat(sccDir); os.IsNotExist(err) {
			continue
		}

		// Run scc
		result, err := b.runner.Run(ctx, sccDir, proj.ExcludeDirs)
		if err != nil {
			b.warn(proj.Name, dateStr, camperrors.Wrap(err, "scc"))
			continue
		}

		// Determine actual people: use config override, or git-detected authors
		projPeople := cfg.ActualPeople
		if projPeople == 0 && proj.AuthorCount > 0 {
			projPeople = proj.AuthorCount
		}
		if projPeople == 0 {
			projPeople = 1
		}

		// Compute leverage score
		score := ComputeScore(result, projPeople, elapsed)
		score.ProjectName = proj.Name
		score.AuthorCount = proj.AuthorCount

		// Override with contribution-based actual person-months
		if cfg.ActualPeople == 0 && proj.ActualPersonMonths > 0 {
			score.ActualPersonMonths = proj.ActualPersonMonths
			estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
			score.FullLeverage = estPM / proj.ActualPersonMonths
		}

		// Get author contributions via git blame
		authors, err := AuthorLOC(ctx, sccDir)
		if err != nil {
			b.warn(proj.Name, dateStr, camperrors.Wrap(err, "blame"))
		}

		// Build and persist snapshot
		scc := SCCResultToSnapshotSCC(result)
		snapshot := NewSnapshot(proj.Name, sample.Hash, sample.Date, time.Now(), scc, score, authors)

		if err := b.store.Save(ctx, snapshot); err != nil {
			return camperrors.Wrapf(err, "saving snapshot for %s", proj.Name)
		}
	}

	return nil
}

// createWorktree creates a detached git worktree and returns the path and cleanup function.
// The cleanup function removes the worktree even if the parent context is cancelled.
//
// On macOS, git worktree add fails with "Undefined error: 0" when the worktree
// is on a different filesystem than the git directory (e.g. /var/folders vs
// /Users). This is especially common with submodules. To avoid this, we create
// the temp directory on the same filesystem as the git repo.
func createWorktree(ctx context.Context, gitDir, commitHash string) (string, func(), error) {
	// Use a temp directory on the same filesystem as the repo to avoid
	// cross-filesystem issues with submodule gitlinks on macOS.
	base := filepath.Join(filepath.Dir(gitDir), ".camp-worktrees")
	if err := os.MkdirAll(base, 0o755); err != nil {
		return "", nil, camperrors.Wrap(err, "creating worktree base dir")
	}

	dir, err := os.MkdirTemp(base, "backfill-*")
	if err != nil {
		return "", nil, camperrors.Wrap(err, "creating temp dir")
	}
	// Remove the temp dir first — git worktree add needs a non-existing path
	os.Remove(dir)

	cmd := exec.CommandContext(ctx, "git", "-C", gitDir, "worktree", "add", "--detach", dir, commitHash)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.RemoveAll(dir)
		return "", nil, camperrors.Wrapf(err, "git worktree add: %s", out)
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

// containsDate checks if a date list contains the given date string.
func containsDate(dates []string, target string) bool {
	return slices.Contains(dates, target)
}
