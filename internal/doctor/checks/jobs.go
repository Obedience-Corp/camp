package checks

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/doctor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// JobsCheck reports on camp's deferred commit queue.
//
// The queue's failure modes are quiet by construction. A deferred commit runs
// after the terminal has returned, so a job that fails, or a worker that dies,
// or a cache directory someone deleted, all look exactly like nothing having
// happened. This check is where they become visible without the user knowing
// the queue exists.
type JobsCheck struct{}

// NewJobsCheck creates a new deferred-queue health check.
func NewJobsCheck() *JobsCheck {
	return &JobsCheck{}
}

// ID returns the check identifier.
func (c *JobsCheck) ID() string { return "jobs" }

// Name returns the human-readable check name.
func (c *JobsCheck) Name() string { return "Deferred Commits" }

// Description returns a brief explanation of what this check does.
func (c *JobsCheck) Description() string {
	return "Reports failed, stuck, and lost deferred commit jobs"
}

// Detail keys, so a --json consumer acts on the row without parsing prose.
const (
	jobsDetailReason = "reason"
	jobsDetailID     = "job_id"
	jobsDetailLane   = "lane"
	jobsDetailPath   = "path"
)

// Row reasons, one per condition this check reports.
const (
	// reasonJobFailed is work camp promised to commit and gave up on.
	reasonJobFailed = "job_failed"
	// reasonJobStuck is a claimed job whose worker is gone. It is not lost:
	// the next worker to take the lane reclaims it.
	reasonJobStuck = "job_stuck"
	// reasonJobStalled is a claimed job whose worker is present and has been
	// on it longer than the writer's budget. Nothing will reclaim this one:
	// the lane is held by a live worker, so the queue reads it as progress.
	reasonJobStalled = "job_stalled"
	// reasonQueueLost is bookkeeping content on disk with no job to commit it,
	// which is what deleting .campaign/cache mid-queue leaves behind.
	reasonQueueLost = "queue_lost"
)

// Run inspects the queue.
func (c *JobsCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	entries, err := jobs.Snapshot(ctx, repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "read deferred commit queue")
	}
	result.Total = len(entries)

	for _, entry := range entries {
		switch {
		case entry.State == "failed":
			result.Issues = append(result.Issues, doctor.Issue{
				Severity: doctor.SeverityError,
				CheckID:  c.ID(),
				Description: fmt.Sprintf("deferred commit failed after %d attempts: %s",
					entry.Attempts, jobs.Describe(entry.Job)),
				FixCommand: "camp jobs retry " + entry.ID,
				Details: map[string]any{
					jobsDetailReason: reasonJobFailed,
					jobsDetailID:     entry.ID,
					jobsDetailLane:   entry.Lane,
				},
			})
		case entry.Stuck:
			result.Issues = append(result.Issues, doctor.Issue{
				Severity: doctor.SeverityWarning,
				CheckID:  c.ID(),
				Description: fmt.Sprintf("deferred commit is claimed but no worker is running it: %s",
					jobs.Describe(entry.Job)),
				FixCommand: "camp jobs run",
				Details: map[string]any{
					jobsDetailReason: reasonJobStuck,
					jobsDetailID:     entry.ID,
					jobsDetailLane:   entry.Lane,
				},
			})
		case entry.Stalled:
			// The quietest failure the queue has. A stuck job at least has an
			// absent worker to notice; this one has a live worker holding the
			// lane, heartbeating, blocking every drain behind it, and looking
			// from the outside exactly like work in progress.
			result.Issues = append(result.Issues, doctor.Issue{
				Severity: doctor.SeverityWarning,
				CheckID:  c.ID(),
				Description: fmt.Sprintf("deferred commit is not making progress (%s): %s",
					entry.StalledReason, jobs.Describe(entry.Job)),
				FixCommand: "camp jobs drop --running " + entry.ID,
				Details: map[string]any{
					jobsDetailReason: reasonJobStalled,
					jobsDetailID:     entry.ID,
					jobsDetailLane:   entry.Lane,
				},
			})
		}
	}

	lost, err := c.uncommittedBookkeeping(ctx, repoRoot, len(entries))
	if err != nil {
		return nil, err
	}
	result.Issues = append(result.Issues, lost...)

	for _, issue := range result.Issues {
		if issue.Severity == doctor.SeverityError {
			result.Passed = false
			break
		}
	}
	return result, nil
}

// uncommittedBookkeeping finds camp's own files sitting uncommitted with no job
// on its way to commit them.
//
// This is the deleted-cache case. The queue lives under .campaign/cache because
// it is derived and disposable, and a user is entitled to delete a cache. Doing
// it mid-queue loses the jobs but never the content: the intent, manifest, or
// marker is already on disk, it simply has nobody coming to commit it. Reported
// as information rather than a problem, because nothing is broken and the fix
// is one ordinary commit.
//
// The check only runs when the queue is empty. A non-empty queue means work is
// still on its way, and every uncommitted bookkeeping file would then be a
// false positive against the jobs that are about to commit it.
func (c *JobsCheck) uncommittedBookkeeping(ctx context.Context, repoRoot string, queued int) ([]doctor.Issue, error) {
	if queued > 0 {
		return nil, nil
	}

	// A repository with no commits has nothing uncommitted in the sense this
	// check means. Everything in a freshly scaffolded campaign is untracked,
	// including camp's own OBEY.md and .gitkeep files, so without this gate the
	// first `camp doctor` in a new campaign reports camp's own scaffolding as
	// lost queue content. The signal only exists relative to a history.
	if !hasCommits(ctx, repoRoot) {
		return nil, nil
	}

	cfg, err := config.LoadCampaignConfig(ctx, repoRoot)
	if err != nil || cfg == nil {
		// Not a campaign, or a config camp cannot read. Either way this check
		// has nothing to say, and the other rows already ran.
		return nil, nil
	}
	resolver := paths.NewResolver(repoRoot, cfg.Paths())

	// The locations camp writes on the user's behalf. Deliberately a short
	// explicit list rather than "anything under .campaign": camp's own cache
	// and machine-local state are gitignored and are supposed to be
	// uncommitted, so a broad rule would report normal operation as a problem.
	prefixes := []string{
		strings.TrimSuffix(filepath.ToSlash(resolver.RelativeIntents()), "/"),
		filepath.ToSlash(filepath.Join(".campaign", "workitems")),
		filepath.ToSlash(filepath.Join(".campaign", "manifests")),
	}

	// -uall so an untracked directory reports its files rather than the
	// directory. A just-captured intent is untracked, and a rolled-up
	// ".campaign/intents/" entry would never match a file prefix.
	out, err := git.StatusPorcelain(ctx, repoRoot, "-uall")
	if err != nil {
		return nil, nil // a repo git cannot read is the git checks' problem
	}

	var found []string
	for _, entry := range git.ParseStatusPorcelainZ(out) {
		path := filepath.ToSlash(entry.Path)
		for _, prefix := range prefixes {
			if prefix != "" && strings.HasPrefix(path, prefix+"/") {
				found = append(found, path)
				break
			}
		}
	}
	if len(found) == 0 {
		return nil, nil
	}

	issues := make([]doctor.Issue, 0, len(found))
	for _, path := range found {
		issues = append(issues, doctor.Issue{
			Severity: doctor.SeverityInfo,
			CheckID:  c.ID(),
			Description: fmt.Sprintf(
				"uncommitted bookkeeping with no queued job: %s", path),
			FixCommand: "camp commit -m \"record pending bookkeeping\"",
			Details: map[string]any{
				jobsDetailReason: reasonQueueLost,
				jobsDetailPath:   path,
			},
		})
	}
	return issues, nil
}

// hasCommits reports whether the repository has any history yet.
func hasCommits(ctx context.Context, repoRoot string) bool {
	_, err := git.Output(ctx, repoRoot, "rev-parse", "--verify", "HEAD")
	return err == nil
}

// Fix is not implemented: nothing this check reports is safe to act on without
// the user deciding.
//
// Retrying a failed job re-runs a commit that already failed once for a reason
// camp does not know, and committing lost bookkeeping writes to the user's
// history. Both have one-line commands, printed on the row that found them, so
// the user chooses rather than discovering afterward that --fix committed
// something.
func (c *JobsCheck) Fix(ctx context.Context, _ string, _ []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return nil, nil
}
