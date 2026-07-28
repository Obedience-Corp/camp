package commitkit

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/jobs"
)

// The deferred queue's public surface, for consumers outside this module.
//
// fest is a separate module and cannot import camp's internal/ packages, so
// this file is the contract it codes against. It is deliberately one function:
// a consumer needs to wait for camp's queued commits before touching history,
// and needs nothing else about the queue. Everything the queue knows about
// lanes, workers, and spawning stays behind it, so camp can change all of it in
// a version bump without fest changing a line.

// DrainJobs waits for camp's deferred commits against repoPath to land.
//
// Camp defers its own bookkeeping commits so they do not hold a terminal. That
// is safe only if whatever reads or writes git history next waits for them, and
// a consumer that stages and commits through this package is exactly such a
// caller: without this, fest could commit on top of a tree camp is still about
// to change, and the ordering would be visible in fest's own history.
//
// It is called from StageAll, StageFiles, and Commit rather than left to the
// caller. Which of those a consumer uses is its own business (fest stages via
// StageFiles and commits some paths raw today), so wiring the drain into all
// three is what makes the guarantee independent of that choice. Doing it more
// than once per operation costs a directory read on the common empty queue,
// which is cheaper than a contract that depends on the caller's shape.
//
// Outside a campaign it is a no-op, so a bare git repository behaves exactly as
// it did before deferral existed.
func DrainJobs(ctx context.Context, repoPath string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	campaignRoot, err := campaign.Detect(ctx, repoPath)
	if err != nil || campaignRoot == "" {
		return nil
	}
	repo := jobs.RepoForPath(campaignRoot, repoPath)
	if repo == "" {
		return nil
	}
	_, err = jobs.Drain(ctx, campaignRoot, repo, jobs.DrainOptions{})
	return err
}

// drainQuietly runs DrainJobs and discards a timeout.
//
// The staging and commit entrypoints call this rather than DrainJobs directly.
// A drain that times out means camp's own queue is slow, which is worth
// reporting where a user can act on it (camp's commands do) but is not grounds
// for failing a consumer's commit: fest's commit is not the thing that went
// wrong, and turning camp's queue latency into fest's error would make a camp
// problem look like a fest bug. A consumer that wants the timeout calls
// DrainJobs itself.
func drainQuietly(ctx context.Context, repoPath string) {
	_ = DrainJobs(ctx, repoPath)
}
