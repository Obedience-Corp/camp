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
