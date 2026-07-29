//go:build integration
// +build integration

package integration

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Obedience-Corp/camp/internal/jobs"
)

// The queue is a filesystem data structure, so its tests mutate real
// directories. They run under the integration tag and inside t.TempDir rather
// than a container because they touch no git and no camp binary: the unit
// under test is the filesystem protocol itself, and the properties that matter
// (atomic create, atomic rename, crash recovery) are properties of the local
// filesystem the worker will actually run against.

func queueRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".campaign", "cache"), 0o755))
	return root
}

// Two enqueuers racing must produce two adjacent numbers rather than one
// number twice. Neither ordering is owed: they are concurrent.
func TestIntegration_JobsEnqueueRaceProducesAdjacentSequences(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	const n = 16
	var wg sync.WaitGroup
	seqs := make([]int, n)
	errs := make([]error, n)

	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // release them together to maximize contention
			job, err := jobs.Enqueue(ctx, root, jobs.Job{
				Kind: jobs.KindCommitPaths, Repo: ".",
				Paths: []string{fmt.Sprintf("f%d.md", i)},
			})
			errs[i] = err
			if job != nil {
				seqs[i] = job.Seq
			}
		}()
	}
	close(start)
	wg.Wait()

	seen := make(map[int]bool, n)
	for i, err := range errs {
		require.NoError(t, err, "enqueuer %d", i)
		require.NotZero(t, seqs[i], "enqueuer %d got no sequence", i)
		assert.False(t, seen[seqs[i]], "sequence %d was allocated twice", seqs[i])
		seen[seqs[i]] = true
	}
	assert.Len(t, seen, n, "each enqueuer must get its own number")

	pending, err := jobs.List(root, "pending", ".")
	require.NoError(t, err)
	assert.Len(t, pending, n, "every job must be durable on disk")
}

// Claim is exclusive: the rename is the election. Racing claimants must never
// both receive the same job, and the loser must move on rather than error.
func TestIntegration_JobsClaimIsExclusiveUnderRace(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	const n = 12
	for i := range n {
		_, err := jobs.Enqueue(ctx, root, jobs.Job{
			Kind: jobs.KindCommitPaths, Repo: ".",
			Paths: []string{fmt.Sprintf("f%d.md", i)},
		})
		require.NoError(t, err)
	}

	var mu sync.Mutex
	claimed := make(map[string]int)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for {
				job, done, err := jobs.Claim(ctx, root, ".")
				if err != nil || job == nil {
					return
				}
				mu.Lock()
				claimed[job.ID]++
				mu.Unlock()
				require.NoError(t, done(nil))
			}
		}()
	}
	close(start)
	wg.Wait()

	assert.Len(t, claimed, n, "every job must be claimed exactly once")
	for id, count := range claimed {
		assert.Equal(t, 1, count, "job %s was claimed %d times", id, count)
	}

	pending, err := jobs.List(root, "pending", ".")
	require.NoError(t, err)
	assert.Empty(t, pending, "completed jobs must be unlinked")
	running, err := jobs.List(root, "running", ".")
	require.NoError(t, err)
	assert.Empty(t, running, "no job may be left claimed")
}

// Failing a job preserves it. A failed job is evidence: it names work camp
// promised to do and did not.
func TestIntegration_JobsFailurePreservesTheJob(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	_, err := jobs.Enqueue(ctx, root, jobs.Job{
		Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	require.NoError(t, err)

	job, done, err := jobs.Claim(ctx, root, ".")
	require.NoError(t, err)
	require.NotNil(t, job)
	require.NoError(t, done(assert.AnError))

	failed, err := jobs.List(root, "failed", ".")
	require.NoError(t, err)
	require.Len(t, failed, 1, "a failed job must be kept for inspection")
	assert.Equal(t, job.ID, failed[0].ID)

	running, err := jobs.List(root, "running", ".")
	require.NoError(t, err)
	assert.Empty(t, running)
}

// A failed job's number must not be reused while it exists, or `camp jobs`
// output becomes ambiguous about which job a number refers to.
func TestIntegration_JobsFailedSequenceIsNotReused(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	first, err := jobs.Enqueue(ctx, root, jobs.Job{
		Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	require.NoError(t, err)

	_, done, err := jobs.Claim(ctx, root, ".")
	require.NoError(t, err)
	require.NoError(t, done(assert.AnError))

	second, err := jobs.Enqueue(ctx, root, jobs.Job{
		Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"b.md"},
	})
	require.NoError(t, err)

	assert.Greater(t, second.Seq, first.Seq,
		"a new job must not reuse a failed job's number while it exists")
}

// Crash recovery. A worker that dies mid-job leaves its file in running/ with
// nothing watching it; without reclaim that job is stranded forever, which is
// the queue's one unforgivable failure.
func TestIntegration_JobsReclaimRecoversStrandedWork(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	_, err := jobs.Enqueue(ctx, root, jobs.Job{
		Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"a.md"},
	})
	require.NoError(t, err)

	// Claim and abandon: the completion func is never called, exactly as a
	// crashed worker would leave it.
	job, _, err := jobs.Claim(ctx, root, ".")
	require.NoError(t, err)
	require.NotNil(t, job)
	assert.Equal(t, 0, job.Attempts)

	// Nothing reclaims a job that might still be running.
	n, err := jobs.Reclaim(ctx, root, ".", time.Hour)
	require.NoError(t, err)
	assert.Equal(t, 0, n, "a job with a fresh heartbeat must not be reclaimed")

	n, err = jobs.Reclaim(ctx, root, ".", 0)
	require.NoError(t, err)
	assert.Equal(t, 1, n, "a stale running job must return to pending")

	pending, err := jobs.List(root, "pending", ".")
	require.NoError(t, err)
	require.Len(t, pending, 1, "the job must be runnable again, not lost")
	assert.Equal(t, 1, pending[0].Attempts, "reclaim must record the attempt")
	assert.Equal(t, job.ID, pending[0].ID, "reclaim must preserve job identity")

	running, err := jobs.List(root, "running", ".")
	require.NoError(t, err)
	assert.Empty(t, running)
}

// Lanes are independent: work in one repo must not serialize against another.
func TestIntegration_JobsLanesAreIndependent(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	for _, repo := range []string{".", "projects/camp", "projects/fest"} {
		for i := range 3 {
			_, err := jobs.Enqueue(ctx, root, jobs.Job{
				Kind: jobs.KindCommitPaths, Repo: repo,
				Paths: []string{fmt.Sprintf("f%d.md", i)},
			})
			require.NoError(t, err)
		}
	}

	// Each lane numbers from 1 independently.
	for _, repo := range []string{".", "projects/camp", "projects/fest"} {
		pending, err := jobs.List(root, "pending", repo)
		require.NoError(t, err)
		require.Len(t, pending, 3, "lane %s", repo)
		assert.Equal(t, 1, pending[0].Seq, "lane %s must number from 1", repo)
		assert.Equal(t, 3, pending[2].Seq, "lane %s", repo)
	}

	// Draining one lane leaves the others untouched.
	for {
		job, done, err := jobs.Claim(ctx, root, "projects/camp")
		require.NoError(t, err)
		if job == nil {
			break
		}
		require.NoError(t, done(nil))
	}

	remaining, err := jobs.List(root, "pending", ".")
	require.NoError(t, err)
	assert.Len(t, remaining, 3, "draining one lane must not touch another")

	lanes, err := jobs.Lanes(root, "pending")
	require.NoError(t, err)
	assert.Contains(t, lanes, jobs.LaneSlug("."))
	assert.Contains(t, lanes, jobs.LaneSlug("projects/fest"))
}

// Execution order within a lane is the sequence, which a lexical sort must
// reproduce even past ten jobs.
func TestIntegration_JobsExecuteInSequenceOrder(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	const n = 12
	for i := range n {
		_, err := jobs.Enqueue(ctx, root, jobs.Job{
			Kind: jobs.KindCommitPaths, Repo: ".",
			Paths: []string{fmt.Sprintf("f%02d.md", i)},
		})
		require.NoError(t, err)
	}

	var order []int
	for {
		job, done, err := jobs.Claim(ctx, root, ".")
		require.NoError(t, err)
		if job == nil {
			break
		}
		order = append(order, job.Seq)
		require.NoError(t, done(nil))
	}

	require.Len(t, order, n)
	for i, seq := range order {
		assert.Equal(t, i+1, seq, "jobs must execute in sequence order, got %v", order)
	}
}

// An enqueue is durable the moment it returns: the file is on disk, readable,
// and carries everything the worker needs.
func TestIntegration_JobsEnqueueIsDurableAndComplete(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	want := jobs.Job{
		Kind: jobs.KindCommitTree, Repo: "projects/camp",
		Tree: "9f2a1b", Parent: "6000fd8f", AutoWrite: true,
		Then: &jobs.Follow{Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"projects/camp"},
			Message: "update camp submodule ref"},
	}
	got, err := jobs.Enqueue(ctx, root, want)
	require.NoError(t, err)
	require.NotEmpty(t, got.ID)
	require.NotEmpty(t, got.CreatedAt)

	stored, err := jobs.List(root, "pending", "projects/camp")
	require.NoError(t, err)
	require.Len(t, stored, 1)

	s := stored[0]
	assert.Equal(t, got.ID, s.ID)
	assert.Equal(t, jobs.KindCommitTree, s.Kind)
	assert.Equal(t, "9f2a1b", s.Tree)
	assert.Equal(t, "6000fd8f", s.Parent)
	assert.True(t, s.AutoWrite)
	require.NotNil(t, s.Then, "the follow-up must survive the round trip")
	assert.Equal(t, []string{"projects/camp"}, s.Then.Paths)
}

// An invalid job never reaches the disk. Once a file exists it is a promise,
// so validation has to happen before the write, not at execution time.
func TestIntegration_JobsInvalidJobIsNeverWritten(t *testing.T) {
	root := queueRoot(t)
	ctx := context.Background()

	_, err := jobs.Enqueue(ctx, root, jobs.Job{
		Kind: jobs.KindCommitPaths, Repo: ".", Paths: []string{"."},
	})
	require.Error(t, err, "a whole-tree path must be rejected")

	pending, err := jobs.List(root, "pending", ".")
	require.NoError(t, err)
	assert.Empty(t, pending, "a rejected job must leave nothing on disk")
}
