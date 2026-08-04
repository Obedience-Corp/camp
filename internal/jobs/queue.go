package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Queue layout, campaign-relative.
const (
	// QueueRelDir is the queue root under the campaign's cache.
	QueueRelDir = ".campaign/cache/jobs"

	statePending = "pending"
	stateRunning = "running"
	stateFailed  = "failed"

	// seqWidth zero-pads the sequence so a lexical directory sort is the
	// execution order. Seven digits outlast any plausible campaign.
	seqWidth = 7
)

// QueueDir returns the absolute queue root for a campaign.
func QueueDir(campaignRoot string) string {
	return filepath.Join(campaignRoot, filepath.FromSlash(QueueRelDir))
}

// laneDir returns the absolute directory for one state of one lane.
func laneDir(campaignRoot, state, repo string) string {
	return filepath.Join(QueueDir(campaignRoot), state, LaneSlug(repo))
}

// Enqueue durably records a job and returns it with its allocated identity.
//
// The sequence is derived from the directories rather than stored, so it can
// never go stale: a counter file would need its own crash-safety story, and a
// stale counter would silently reorder execution. failed/ participates in the
// scan precisely so a failed job's number is not reused while it still exists,
// which keeps `camp jobs` output unambiguous.
//
// Two enqueuers racing produce two adjacent numbers in an arbitrary order.
// That is correct rather than merely tolerable: they are concurrent, so no
// ordering between them is owed. Ordering matters only within one enqueuer's
// own sequence of commands, and the user's shell already serializes those.
func Enqueue(ctx context.Context, campaignRoot string, job Job) (*Job, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if err := job.Validate(); err != nil {
		return nil, err
	}

	dir := laneDir(campaignRoot, statePending, job.Repo)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, camperrors.Wrapf(err, "create job lane %s", dir)
	}

	now := time.Now().UTC()
	if job.CreatedAt == "" {
		job.CreatedAt = now.Format("2006-01-02T15:04:05.000Z")
	}

	// Retry on collision: another enqueuer may take the number between our
	// scan and our create. Bounded so a pathological loop cannot spin forever.
	const maxAttempts = 32
	for range maxAttempts {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		next, err := nextSeq(campaignRoot, job.Repo)
		if err != nil {
			return nil, err
		}
		job.Seq = next
		job.ID = newJobID(now)

		path := filepath.Join(dir, jobFilename(job.Seq))
		data, err := json.MarshalIndent(&job, "", "  ")
		if err != nil {
			return nil, camperrors.Wrap(err, "encode job")
		}

		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			if os.IsExist(err) {
				continue // lost the race for this number; re-scan and retry
			}
			return nil, camperrors.Wrapf(err, "create job file %s", path)
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			_ = os.Remove(path)
			return nil, camperrors.Wrapf(err, "write job file %s", path)
		}
		if err := f.Close(); err != nil {
			_ = os.Remove(path)
			return nil, camperrors.Wrapf(err, "close job file %s", path)
		}
		return &job, nil
	}
	return nil, camperrors.Newf("could not allocate a job sequence for lane %q after %d attempts",
		job.Repo, maxAttempts)
}

// nextSeq returns one past the highest sequence present in any state of a lane.
func nextSeq(campaignRoot, repo string) (int, error) {
	highest := 0
	for _, state := range []string{statePending, stateRunning, stateFailed} {
		entries, err := os.ReadDir(laneDir(campaignRoot, state, repo))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return 0, camperrors.Wrapf(err, "scan %s lane for %s", state, repo)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			if n, ok := seqFromFilename(e.Name()); ok && n > highest {
				highest = n
			}
		}
	}
	return highest + 1, nil
}

// Claim moves the lowest-numbered pending job in a lane to running and returns
// it with a completion func.
//
// The rename is the whole election mechanism: it is atomic on POSIX, so two
// racing claimants cannot both take the same job. The loser sees ENOENT and
// moves to the next file rather than failing, because a claim race is normal
// operation, not an error.
//
// The returned func must be called exactly once. Passing nil completes the job
// (unlink); passing an error fails it (rename to failed/), preserving the file
// for inspection.
func Claim(ctx context.Context, campaignRoot, repo string) (*Job, func(error) error, error) {
	if ctx.Err() != nil {
		return nil, nil, ctx.Err()
	}

	pendingDir := laneDir(campaignRoot, statePending, repo)
	names, err := sortedJobFiles(pendingDir)
	if err != nil {
		return nil, nil, err
	}

	runningDir := laneDir(campaignRoot, stateRunning, repo)
	if len(names) > 0 {
		if err := os.MkdirAll(runningDir, 0o755); err != nil {
			return nil, nil, camperrors.Wrapf(err, "create running lane %s", runningDir)
		}
	}

	for _, name := range names {
		if ctx.Err() != nil {
			return nil, nil, ctx.Err()
		}
		src := filepath.Join(pendingDir, name)
		dst := filepath.Join(runningDir, name)

		if err := os.Rename(src, dst); err != nil {
			if os.IsNotExist(err) {
				continue // another claimant took it; try the next
			}
			return nil, nil, camperrors.Wrapf(err, "claim job %s", src)
		}

		job, err := readJob(dst)
		if err == nil {
			// Re-validated at the moment of use, not only at enqueue. A queue
			// file is plain JSON on disk and nothing stops it being edited, so
			// the invariants are only real if they are checked again here. The
			// one that matters most is the explicit-paths rule: an edited
			// `paths: ["."]` would make the worker stage everything present
			// when it runs, which is exactly the sweep the rule prevents.
			//
			// Checked here rather than in readJob because List uses that, and
			// a job that fails validation must still appear in `camp jobs` and
			// still hold a drain. Hiding it from the listing would leave it
			// sitting in pending forever with nothing able to report it.
			err = job.Validate()
		}
		if err != nil {
			// Unreadable or invalid, so it can never execute. Move it to
			// failed rather than leaving it claimed forever, and say so.
			_ = failJobFile(campaignRoot, repo, dst, name)
			return nil, nil, camperrors.Wrapf(err, "job %s cannot run", name)
		}
		return job, completionFor(campaignRoot, repo, dst, name), nil
	}
	return nil, nil, nil
}

// completionFor builds the completion closure for a claimed job.
func completionFor(campaignRoot, repo, runningPath, name string) func(error) error {
	return func(jobErr error) error {
		if jobErr != nil {
			return parkFailedJob(campaignRoot, repo, runningPath, name)
		}
		if err := os.Remove(runningPath); err != nil && !os.IsNotExist(err) {
			return camperrors.Wrapf(err, "complete job %s", runningPath)
		}
		return nil
	}
}

// parkFailedJob records the run and moves the job to failed/.
//
// An attempt is a job that started and did not finish, and until now only one
// of the two ways that happens was counted. Reclaim counted the worker that
// died; a job that ran and returned an error counted nothing, so it was parked
// still reading Attempts: 0 while the listing beside it said the job had
// already been tried and given up on. The two disagreed in the same output,
// and an agent reading --json could not tell a failure that had been retried
// from one that never was.
//
// The incremented copy is written to the destination and the source removed
// after, never edited in place. Reclaim skips a job it cannot parse, so an
// interrupted in-place write would leave unreadable JSON in running/ that
// nothing ever picks up again: the one failure this queue treats as
// unforgivable. Written this way a crash in the window leaves the job in both
// lanes instead, and reclaim resolves that by running it again. Duplicating a
// job is recoverable; stranding one is not.
//
// A job that cannot be read or marshalled is still parked, without a count.
// It has to leave running/ either way, and a wrong number in a listing costs
// less than a job nobody is coming for.
func parkFailedJob(campaignRoot, repo, runningPath, name string) error {
	job, err := readJob(runningPath)
	if err != nil {
		return failJobFile(campaignRoot, repo, runningPath, name)
	}
	job.Attempts++
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return failJobFile(campaignRoot, repo, runningPath, name)
	}

	failedDir := laneDir(campaignRoot, stateFailed, repo)
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create failed lane %s", failedDir)
	}
	if err := os.WriteFile(filepath.Join(failedDir, name), data, 0o644); err != nil {
		return camperrors.Wrapf(err, "park job %s", runningPath)
	}
	if err := os.Remove(runningPath); err != nil && !os.IsNotExist(err) {
		return camperrors.Wrapf(err, "park job %s", runningPath)
	}
	return nil
}

// failJobFile moves a running job to the failed lane, keeping it for
// inspection. A failed job is evidence, not garbage: it names work camp
// promised to do and did not.
func failJobFile(campaignRoot, repo, runningPath, name string) error {
	failedDir := laneDir(campaignRoot, stateFailed, repo)
	if err := os.MkdirAll(failedDir, 0o755); err != nil {
		return camperrors.Wrapf(err, "create failed lane %s", failedDir)
	}
	if err := os.Rename(runningPath, filepath.Join(failedDir, name)); err != nil {
		return camperrors.Wrapf(err, "fail job %s", runningPath)
	}
	return nil
}

// Reclaim returns running jobs older than olderThan to pending, incrementing
// attempts.
//
// This is crash recovery. A worker that dies mid-job leaves its file in
// running/ with nothing watching it, and without reclaim that job is stranded
// forever: the queue's one unforgivable failure. Age is the signal because a
// live worker heartbeats its file, so a stale mtime means nobody is home.
func Reclaim(ctx context.Context, campaignRoot, repo string, olderThan time.Duration) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	runningDir := laneDir(campaignRoot, stateRunning, repo)
	names, err := sortedJobFiles(runningDir)
	if err != nil {
		return 0, err
	}
	if len(names) == 0 {
		return 0, nil
	}

	pendingDir := laneDir(campaignRoot, statePending, repo)
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		return 0, camperrors.Wrapf(err, "create pending lane %s", pendingDir)
	}

	reclaimed := 0
	cutoff := time.Now().Add(-olderThan)
	for _, name := range names {
		path := filepath.Join(runningDir, name)
		info, statErr := os.Stat(path)
		if statErr != nil || info.ModTime().After(cutoff) {
			continue // gone, or still heartbeating
		}

		job, readErr := readJob(path)
		if readErr != nil {
			continue
		}
		job.Attempts++
		data, marshalErr := json.MarshalIndent(job, "", "  ")
		if marshalErr != nil {
			continue
		}
		// Write the incremented job into pending under the same name, then
		// drop the running copy. Written before the unlink so a crash between
		// the two duplicates a job rather than losing it: a job that runs
		// twice is recoverable, a job that never runs is not.
		dst := filepath.Join(pendingDir, name)
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			continue
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			continue
		}
		reclaimed++
	}
	return reclaimed, nil
}

// List returns the jobs in one state of one lane, in execution order.
func List(campaignRoot, state, repo string) ([]Job, error) {
	dir := laneDir(campaignRoot, state, repo)
	names, err := sortedJobFiles(dir)
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(names))
	for _, name := range names {
		job, err := readJob(filepath.Join(dir, name))
		if err != nil {
			continue // an unreadable file is reported by the worker, not here
		}
		out = append(out, *job)
	}
	return out, nil
}

// Lanes returns the repo slugs that have jobs in a given state.
func Lanes(campaignRoot, state string) ([]string, error) {
	entries, err := os.ReadDir(filepath.Join(QueueDir(campaignRoot), state))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "list %s lanes", state)
	}
	var lanes []string
	for _, e := range entries {
		if e.IsDir() {
			lanes = append(lanes, e.Name())
		}
	}
	sort.Strings(lanes)
	return lanes, nil
}

// sortedJobFiles returns job filenames in a directory, lexically sorted, which
// is execution order because filenames lead with the zero-padded sequence.
func sortedJobFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, camperrors.Wrapf(err, "read job directory %s", dir)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// marshalJob encodes a job the way Enqueue does, so a requeued file is
// byte-comparable with one written fresh.
func marshalJob(job *Job) ([]byte, error) {
	data, err := json.MarshalIndent(job, "", "  ")
	if err != nil {
		return nil, camperrors.Wrapf(err, "encode job %s", job.ID)
	}
	return data, nil
}

func readJob(path string) (*Job, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, camperrors.Wrapf(err, "read job %s", path)
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return nil, camperrors.Wrapf(err, "parse job %s", path)
	}
	return &job, nil
}

// jobFilename builds "NNNNNNN.json".
//
// The name is a function of the sequence alone, and that is what makes
// O_CREATE|O_EXCL a working collision check. An earlier shape carried the job
// id (which contains a timestamp and random suffix) in the filename: two
// enqueuers landing on the same sequence then produced two *different* names,
// so neither create collided and both "won" the same number. A race test found
// sixteen concurrent enqueuers sharing three sequences.
//
// The readable id still exists; it lives in the document, which is where every
// consumer reads it from anyway.
func jobFilename(seq int) string {
	return padSeq(seq) + ".json"
}

func padSeq(seq int) string {
	s := strconv.Itoa(seq)
	for len(s) < seqWidth {
		s = "0" + s
	}
	return s
}

// seqFromFilename reads the zero-padded sequence from a job filename.
func seqFromFilename(name string) (int, bool) {
	stem := strings.TrimSuffix(name, ".json")
	if stem == name || stem == "" {
		return 0, false
	}
	n, err := strconv.Atoi(stem)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

// newJobID builds "job-<utc-stamp>-<rand>". The timestamp comes from the
// enqueuing process's clock; there is deliberately no other identity scheme,
// so a job's name is readable and sortable without consulting anything else.
func newJobID(now time.Time) string {
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		// A failed read must not block an enqueue. The sequence already
		// guarantees uniqueness within a lane; the suffix only disambiguates
		// identical timestamps for a human reading the directory.
		return "job-" + now.Format("20060102T150405Z") + "-0000"
	}
	return "job-" + now.Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}
