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
	// name. Both path components were validated at claim, and the root is
	// re-checked at the moment of use because the walk is about to happen.
	machine := job.Machine
	if _, err := artifacts.EnsureRootWithin(campaignRoot, job.ManifestRoot); err != nil {
		return err
	}
	// The baseline is the record as git has it, never the working-tree file:
	// a crashed attempt leaves its write uncommitted, and comparing against
	// that would let the retry skip the commit forever.
	prev, _, err := artifacts.LoadCommittedAtHead(ctx, campaignRoot, machine, job.ManifestRoot)
	if err != nil {
		return err
	}
	m, err := artifacts.BuildManifest(ctx, campaignRoot, job.ManifestRoot)
	if err != nil {
		return err
	}

	// The heartbeat goroutine on the lane lock keeps the worker alive through
	// a long first-pass hash; no progress plumbing is needed for liveness.
	outcome, err := artifacts.HashManifest(ctx, campaignRoot, m, prev, nil)
	if err != nil {
		return err
	}

	if prev != nil && artifacts.SameFiles(prev.Files, m.Files) {
		// HEAD already records this exact artifact state, so there is nothing
		// new to commit. The working-tree file can still hold a crashed
		// attempt's write, which HEAD never accepted: status reads that file,
		// and a later broad commit would sweep it into history as the record.
		// Put it back before calling the job done.
		restored, rErr := artifacts.ReconcileCommittedToHead(ctx, campaignRoot, machine, job.ManifestRoot)
		if rErr != nil {
			return rErr
		}
		if restored {
			logWorker(campaignRoot, "manifest-restore root=%s: discarded a working-tree record HEAD does not carry",
				job.ManifestRoot)
		}
		return nil
	}

	// The stat fingerprint captured at enqueue is what ties the observation to
	// the described commit, and it is compared against a walk taken AFTER the
	// hash pass. A comparison made before hashing proves nothing about the
	// record actually written: the hash pass reads every changed byte and can
	// run for minutes on a first pass, and an entry whose hash was carried
	// forward from prev is never re-read at all, so only a fresh walk can tell
	// that its file has since moved. If the root moved, the pre-edit bytes are
	// gone and no record of them can be made; the truthful record is the
	// observed state anchored to the newest commit at observation time, said out
	// loud in the worker log.
	describes := job.DescribesCommit
	if job.StateFingerprint != "" {
		observed, oErr := artifacts.BuildManifest(ctx, campaignRoot, job.ManifestRoot)
		if oErr != nil {
			return oErr
		}
		if artifacts.FingerprintFiles(observed.Files) != job.StateFingerprint {
			head, headErr := git.FullHash(ctx, repoPath)
			if headErr != nil {
				return headErr
			}
			logWorker(campaignRoot, "manifest-relabel root=%s described=%s now=%s: the root changed after enqueue; the record reflects the observed state",
				job.ManifestRoot, shortSHA(job.DescribesCommit), shortSHA(head))
			describes = head
		}
	}

	// A file being written while it was hashed has no single set of bytes to
	// record, so its entry carries an unknown hash and the next pass re-reads
	// it. Silence here would look identical to a complete record.
	if len(outcome.Unsettled) > 0 {
		logWorker(campaignRoot, "manifest-unsettled root=%s files=%d first=%s: written while being hashed; recorded with hash unknown",
			job.ManifestRoot, len(outcome.Unsettled), outcome.Unsettled[0])
	}

	rel, err := artifacts.WriteCommitted(campaignRoot, machine, m, describes)
	if err != nil {
		return err
	}
	err = git.CommitScoped(ctx, repoPath, []string{rel}, &git.CommitOptions{
		Message: "manifest: " + job.ManifestRoot + " at " + shortSHA(describes),
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
	// artifacts.yaml is hand-editable; a root that escapes the campaign is
	// refused before it can become a queued promise to walk it.
	if err := artifacts.ValidateRootPath(rootRel); err != nil {
		return err
	}
	if _, err := artifacts.EnsureRootWithin(campaignRoot, rootRel); err != nil {
		return err
	}
	// One stat walk, no content reads: the fingerprint is what lets the
	// worker prove the root still holds what it held at this commit.
	snapshot, err := artifacts.BuildManifest(ctx, campaignRoot, rootRel)
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
		Kind:             KindManifest,
		Class:            ClassManifest,
		Repo:             ".",
		ManifestRoot:     artifacts.NormalizeRootPath(rootRel),
		DescribesCommit:  describesCommit,
		Machine:          machine,
		StateFingerprint: artifacts.FingerprintFiles(snapshot.Files),
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
