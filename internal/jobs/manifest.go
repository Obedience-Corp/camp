package jobs

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/git"
)

// Committed artifact manifests ride the queue as their own kind because they
// invert the usual promise. A commit job must land exactly what was captured;
// a manifest job must land whatever the root holds when it runs, labelled
// with the commit it describes. Late is correct here, which is why the class
// is exempt from every drain.

// executeManifest builds the incremental manifest for one root, writes the
// machine's committed record, and commits it. An unchanged root writes and
// commits nothing: the committed record is already correct, and rewriting it
// would churn a byte-identical file through history.
func executeManifest(ctx context.Context, campaignRoot, repoPath string, job *Job) error {
	// The identity was captured at enqueue; a worker spawned under any other
	// environment still writes the enqueuer's record under the enqueuer's
	// name.
	machine := job.Machine
	prev, _, err := artifacts.LoadCommitted(campaignRoot, machine, job.ManifestRoot)
	if err != nil {
		return err
	}
	m, err := artifacts.BuildManifest(ctx, campaignRoot, job.ManifestRoot)
	if err != nil {
		return err
	}
	// The heartbeat goroutine on the lane lock keeps the worker alive through
	// a long first-pass hash; no progress plumbing is needed for liveness.
	if err := artifacts.HashManifest(ctx, campaignRoot, m, prev, nil); err != nil {
		return err
	}
	if prev != nil && artifacts.SameFiles(prev.Files, m.Files) {
		return nil
	}
	rel, err := artifacts.WriteCommitted(campaignRoot, machine, m, job.DescribesCommit)
	if err != nil {
		return err
	}
	err = git.CommitScoped(ctx, repoPath, []string{rel}, &git.CommitOptions{
		Message: "manifest: " + job.ManifestRoot + " at " + shortSHA(job.DescribesCommit),
	})
	if errors.Is(err, git.ErrNoChanges) {
		return nil // a drain or ordinary commit already carried the file
	}
	return err
}

// EnqueueManifest queues one root's manifest job, dropping any pending
// manifest job for the same root first. Only the newest record matters: a
// pending job for an older commit would build the same bytes and label them
// with a stale SHA, so the dedupe keeps the latest describes_commit and the
// queue from accumulating one job per keystroke.
func EnqueueManifest(ctx context.Context, campaignRoot, rootRel, describesCommit string) error {
	machine, err := artifacts.MachineName()
	if err != nil {
		return err
	}
	pending, err := List(campaignRoot, statePending, ".")
	if err == nil {
		for _, j := range pending {
			if j.Kind == KindManifest && j.ManifestRoot == artifacts.NormalizeRootPath(rootRel) && j.Machine == machine {
				// A claim racing this remove wins the rename and the remove
				// sees ENOENT; both jobs then run and the newer record lands
				// last. Duplicate work, never a wrong record.
				_ = os.Remove(filepath.Join(laneDir(campaignRoot, statePending, "."), jobFilename(j.Seq)))
			}
		}
	}
	_, err = Enqueue(ctx, campaignRoot, Job{
		Kind:            KindManifest,
		Class:           ClassManifest,
		Repo:            ".",
		ManifestRoot:    artifacts.NormalizeRootPath(rootRel),
		DescribesCommit: describesCommit,
		Machine:         machine,
	})
	return err
}

// EnqueueManifestsForRoots queues a manifest job per declared root, recording
// the campaign root's current HEAD as the described commit. A campaign with
// no declared roots does nothing at all, which is the zero cost everyone else
// pays for this feature.
func EnqueueManifestsForRoots(ctx context.Context, campaignRoot string) (int, error) {
	cfg, err := artifacts.Load(campaignRoot)
	if err != nil {
		return 0, err
	}
	if len(cfg.Roots) == 0 {
		return 0, nil
	}
	head, err := git.FullHash(ctx, campaignRoot)
	if err != nil {
		return 0, err
	}
	enqueued := 0
	for _, r := range cfg.Roots {
		if ctx.Err() != nil {
			return enqueued, ctx.Err()
		}
		if err := EnqueueManifest(ctx, campaignRoot, r.Path, head); err != nil {
			return enqueued, err
		}
		enqueued++
	}
	return enqueued, nil
}
