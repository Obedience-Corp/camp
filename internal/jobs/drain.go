package jobs

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Drain timing.
//
// Variables rather than constants so tests drive time instead of sleeping
// through it. Every one of these is a user-facing latency decision, so they are
// gathered here rather than scattered through the wait loop.
var (
	// drainQuiet is how long a drain stays silent. The common case is an empty
	// queue, where a drain is one directory read and finishes far inside this
	// window; printing before it would put a spinner on every camp command for
	// work that is already done.
	drainQuiet = 250 * time.Millisecond
	// drainPollMin and drainPollMax bound the poll backoff. Starting tight
	// makes the ordinary "worker is nearly done" case feel instant; backing off
	// keeps a long wait from spinning the disk.
	drainPollMin = 50 * time.Millisecond
	drainPollMax = 250 * time.Millisecond
	// drainProgressEvery re-renders the waiting line so a user watching a slow
	// drain sees the count fall rather than one frozen message.
	drainProgressEvery = 2 * time.Second
	// DrainTimeout is the default ceiling for a drain.
	DrainTimeout = 30 * time.Second
	// drainMaxWait bounds a job-aware drain. Job-aware means "keep waiting
	// while the lane is heartbeating", and a wedged worker heartbeats forever,
	// so without a ceiling the promise to wait for progress becomes a hang. Five
	// minutes is well past any real message writer and still finite.
	drainMaxWait = 5 * time.Minute
	// drainSpawnRecheck throttles the spawn check inside the wait loop. The
	// first check happens immediately; re-checking is only for the case where
	// the serving worker dies mid-drain, which is rare enough not to warrant a
	// process-start check on every poll.
	drainSpawnRecheck = 2 * time.Second
)

// DrainStatus is what a drain is waiting on, handed to the progress callback.
type DrainStatus struct {
	// Blocking are the jobs still outstanding, in execution order.
	Blocking []Job
	// Elapsed is how long the drain has been waiting.
	Elapsed time.Duration
}

// DrainOptions configures one drain.
type DrainOptions struct {
	// Timeout bounds the wait. Zero means DrainTimeout.
	Timeout time.Duration
	// JobAware extends the wait past Timeout while the lane holding the
	// blocking job is still heartbeating, up to drainMaxWait.
	//
	// The commit drain point sets it. A deferred --auto-write message writer is
	// unbounded (it may be an LLM call), so `camp commit -aw` followed by
	// `camp commit -m` inside the writer's window would otherwise refuse the
	// second commit through no fault of the user.
	JobAware bool
	// OnWaiting is called once the drain has been waiting longer than
	// drainQuiet, then every drainProgressEvery. Nil renders nothing.
	OnWaiting func(DrainStatus)
}

// DrainResult reports what a drain did.
type DrainResult struct {
	// Waited is the wall time the drain spent. Reported as drain_waited_ms by
	// every --json command that drains, so an agent can see the queue's cost on
	// its critical path rather than inferring it from total runtime.
	Waited time.Duration
	// Spawned names the lanes this drain started a worker for.
	Spawned []string
}

// DrainTimeoutError reports a drain that gave up with jobs still outstanding.
//
// It carries the jobs rather than a rendered string because the two callers
// need opposite things from them: a write command refuses and names them, a
// read-only command warns and proceeds. Formatting here would force both to
// parse prose.
type DrainTimeoutError struct {
	// Repo is the lane that was being drained.
	Repo string
	// Blocking are the jobs still outstanding, in execution order.
	Blocking []Job
	// Waited is how long the drain waited before giving up.
	Waited time.Duration
}

func (e *DrainTimeoutError) Error() string {
	n := len(e.Blocking)
	noun, verb := "commits", "have"
	if n == 1 {
		noun, verb = "commit", "has"
	}
	return fmt.Sprintf("%d queued %s %s not completed after %s in %s",
		n, noun, verb, e.Waited.Round(time.Second), e.Repo)
}

// Drain blocks until no job a command depends on is outstanding for repo.
//
// Deferral is only safe if the commands that read or write git history wait for
// it. Without this, `camp intent add` followed by `camp push` pushes a branch
// missing the intent and the user has no way to know. A drain is the barrier
// that makes the first command's promise true before the second one runs.
//
// It spawns a worker when one is needed. A drain that only waits can block for
// its whole timeout against a queue nobody is serving, which turns a crashed
// worker into a wedged terminal.
//
// Outside a campaign, or with an empty queue, this is one directory read and
// returns immediately: every camp command that touches history calls it, so the
// no-op path has to be cheap enough to be invisible.
func Drain(ctx context.Context, campaignRoot, repo string, opts DrainOptions) (DrainResult, error) {
	if repo == "" {
		return DrainResult{}, nil
	}
	return drainWith(ctx, campaignRoot, repo, opts, func() ([]Job, error) {
		return Outstanding(campaignRoot, repo)
	})
}

// DrainAll waits for every lane in the campaign.
//
// Used by the commands that operate on the whole campaign at once
// (`camp push all`, `camp pull all`, `camp jobs drain`). A per-repo drain would
// be wrong there in a way the user would only find later: it would let a
// submodule's queued commit land after the run that was supposed to publish it.
func DrainAll(ctx context.Context, campaignRoot string, opts DrainOptions) (DrainResult, error) {
	return drainWith(ctx, campaignRoot, "all lanes", opts, func() ([]Job, error) {
		return OutstandingAll(campaignRoot)
	})
}

// drainWith is the wait loop, parameterized by what counts as outstanding.
func drainWith(ctx context.Context, campaignRoot, label string, opts DrainOptions,
	outstanding func() ([]Job, error),
) (DrainResult, error) {
	var result DrainResult
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if campaignRoot == "" {
		return result, nil
	}

	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = DrainTimeout
	}

	start := time.Now()
	blocking, err := outstanding()
	if err != nil || len(blocking) == 0 {
		return result, err
	}

	result.Spawned = spawnForJobs(ctx, campaignRoot, blocking)

	var (
		announced    bool
		lastProgress time.Time
		lastSpawn    = time.Now()
		poll         = drainPollMin
	)
	for {
		select {
		case <-ctx.Done():
			result.Waited = time.Since(start)
			return result, ctx.Err()
		case <-time.After(poll):
		}
		if poll < drainPollMax {
			poll = min(poll*2, drainPollMax)
		}

		blocking, err = outstanding()
		if err != nil {
			return result, err
		}
		result.Waited = time.Since(start)
		if len(blocking) == 0 {
			return result, nil
		}

		if opts.OnWaiting != nil && result.Waited >= drainQuiet &&
			(!announced || time.Since(lastProgress) >= drainProgressEvery) {
			opts.OnWaiting(DrainStatus{Blocking: blocking, Elapsed: result.Waited})
			announced = true
			lastProgress = time.Now()
		}

		if time.Since(lastSpawn) >= drainSpawnRecheck {
			result.Spawned = append(result.Spawned, spawnForJobs(ctx, campaignRoot, blocking)...)
			lastSpawn = time.Now()
		}

		if result.Waited < timeout {
			continue
		}
		// Past the deadline. A job-aware drain keeps waiting only while the
		// lane is heartbeating, which is the difference between "slow" and
		// "abandoned": a live worker will finish, a dead one never will.
		if opts.JobAware && result.Waited < drainMaxWait &&
			laneLockFresh(QueueDir(campaignRoot), LaneSlug(blocking[0].Repo)) {
			continue
		}
		return result, &DrainTimeoutError{Repo: label, Blocking: blocking, Waited: result.Waited}
	}
}

// Outstanding returns the jobs a drain of repo waits for, in execution order.
//
// Two things are outside repo's own lane and still counted.
//
// Running jobs count, not only pending ones: a job that has been claimed has
// not landed yet, and a drain that ignored running/ would return exactly when
// the commit is most likely to be mid-flight.
//
// A root drain also counts jobs in other lanes whose follow-up targets the
// root. When a submodule job lands, the worker enqueues a gitlink-recording
// job into the root lane; a root drain that saw only the root lane could find
// it empty, return, and have that follow-up appear immediately afterward. The
// job that has not run yet is the one that proves the root lane is not done.
func Outstanding(campaignRoot, repo string) ([]Job, error) {
	target := normalizeRepo(repo)
	var out []Job

	for _, state := range []string{statePending, stateRunning} {
		jobs, err := List(campaignRoot, state, target)
		if err != nil {
			return nil, err
		}
		for _, job := range jobs {
			if job.Class.Blocking() {
				out = append(out, job)
			}
		}
	}
	if target != "." {
		return out, nil
	}

	for _, state := range []string{statePending, stateRunning} {
		slugs, err := Lanes(campaignRoot, state)
		if err != nil {
			return nil, err
		}
		for _, slug := range slugs {
			lane := RepoFromLaneSlug(slug)
			if normalizeRepo(lane) == "." {
				continue // already counted above
			}
			jobs, err := List(campaignRoot, state, lane)
			if err != nil {
				return nil, err
			}
			for _, job := range jobs {
				if job.Class.Blocking() && job.Then != nil &&
					normalizeRepo(job.Then.Repo) == "." {
					out = append(out, job)
				}
			}
		}
	}
	return out, nil
}

// OutstandingAll returns the blocking jobs in every lane, ordered by lane then
// sequence.
//
// The cross-lane follow-up rule Outstanding needs does not apply here: a
// follow-up targets a lane this already covers, so counting the parent is
// enough.
func OutstandingAll(campaignRoot string) ([]Job, error) {
	seen := make(map[string]struct{})
	var repos []string
	for _, state := range []string{statePending, stateRunning} {
		slugs, err := Lanes(campaignRoot, state)
		if err != nil {
			return nil, err
		}
		for _, slug := range slugs {
			repo := RepoFromLaneSlug(slug)
			if _, ok := seen[repo]; ok {
				continue
			}
			seen[repo] = struct{}{}
			repos = append(repos, repo)
		}
	}
	sort.Strings(repos)

	var out []Job
	for _, repo := range repos {
		for _, state := range []string{statePending, stateRunning} {
			jobs, err := List(campaignRoot, state, repo)
			if err != nil {
				return nil, err
			}
			for _, job := range jobs {
				if job.Class.Blocking() {
					out = append(out, job)
				}
			}
		}
	}
	return out, nil
}

// spawnForJobs starts a worker for each lane holding outstanding work.
//
// Per lane rather than per job, and via SpawnIfNeeded, so a lane that already
// has a live worker is left alone and the cap still applies. A root drain may
// be waiting on two lanes at once (the root's own and a submodule's follow-up
// parent), and waiting on a lane nobody serves is the failure this exists to
// prevent.
// EnsureServed starts a worker for any lane in blocking that has none, without
// waiting for the work to finish.
//
// Exported for the reporting commands, which announce the queue rather than
// waiting for it. They still owe it a kick: spawning is what makes "a queued
// job is always eventually served" true, and it is the cheap half of a drain.
// A user whose worker died and who only ever runs `camp status` would
// otherwise watch the same jobs sit there forever, being told about them each
// time and never seeing them move.
func EnsureServed(ctx context.Context, campaignRoot string, blocking []Job) []string {
	return spawnForJobs(ctx, campaignRoot, blocking)
}

func spawnForJobs(ctx context.Context, campaignRoot string, blocking []Job) []string {
	seen := make(map[string]struct{}, len(blocking))
	var spawned []string
	for _, job := range blocking {
		lane := normalizeRepo(job.Repo)
		if _, ok := seen[lane]; ok {
			continue
		}
		seen[lane] = struct{}{}
		if laneLockFresh(QueueDir(campaignRoot), LaneSlug(lane)) {
			continue
		}
		spawnLane(ctx, campaignRoot, lane)
		spawned = append(spawned, lane)
	}
	return spawned
}

// spawnLane is how a drain starts a worker. It is a variable so tests can
// assert the spawn decision without starting real processes; SpawnIfNeeded is
// always what runs in production.
var spawnLane = SpawnIfNeeded

// Describe names a job in one short phrase, for the waiting line and the
// refusal that follows it.
//
// The message names the work rather than saying "please wait", so a user who
// sees it repeatedly learns what camp is doing on their behalf instead of
// learning to ignore it.
func Describe(job Job) string {
	if subject := firstLine(job.Message); subject != "" {
		return subject
	}
	if job.Class == ClassManifest {
		return "artifact manifest for " + job.Repo
	}
	if len(job.Paths) > 0 {
		return job.Paths[0] + " in " + job.Repo
	}
	if job.Kind == KindCommitTree && job.AutoWrite {
		return "writing a commit message in " + job.Repo
	}
	return string(job.Kind) + " in " + job.Repo
}

// AttemptNote renders a job's attempt count, or "" when there is nothing worth
// saying.
//
// The two states need opposite tenses, and conflating them produces the
// nonsense "attempt 4 of 3": a job parked in failed/ has already used all
// MaxAttempts tries, so counting the next one describes a run that will never
// happen. A pending job's count is forward-looking, a failed job's is final.
func AttemptNote(attempts int, failed bool) string {
	switch {
	case failed:
		if attempts <= 1 {
			return "gave up after 1 attempt"
		}
		return fmt.Sprintf("gave up after %d attempts", attempts)
	case attempts <= 0:
		return ""
	default:
		return fmt.Sprintf("attempt %d of %d", attempts+1, MaxAttempts)
	}
}

// firstLine returns the first non-empty line of a message, trimmed.
func firstLine(message string) string {
	for line := range strings.SplitSeq(message, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// RepoForPath maps an absolute repository path to its campaign-relative lane,
// returning "" when the path is not inside the campaign.
//
// Both sides are resolved through symlinks before comparison. On macOS a
// campaign under /tmp is reached by two spellings of the same directory, and
// comparing them raw makes filepath.Rel emit "..", which would silently drop
// the drain for a repo that is in fact inside the campaign.
func RepoForPath(campaignRoot, repoPath string) string {
	if campaignRoot == "" || repoPath == "" {
		return ""
	}
	rel, err := filepath.Rel(resolveDir(campaignRoot), resolveDir(repoPath))
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return ""
	}
	return normalizeRepo(rel)
}

// resolveDir makes a path absolute and resolves symlinks, resolving the
// deepest ancestor that exists and re-appending the rest.
//
// Falling back to the unresolved absolute path is not good enough here. On
// macOS a campaign under /tmp resolves to /private/var/..., so a repo path that
// does not exist yet would keep its /var/... spelling while the root got the
// resolved one. filepath.Rel between those two produces "..", and RepoForPath
// would report "not in this campaign" for a repo that plainly is: the drain
// would then be skipped with no error and no output, which is the worst way for
// an ordering barrier to fail.
func resolveDir(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		abs = p
	}
	rest := ""
	for cur := abs; ; {
		if resolved, err := filepath.EvalSymlinks(cur); err == nil {
			return filepath.Join(resolved, rest)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs // reached the volume root without finding anything
		}
		rest = filepath.Join(filepath.Base(cur), rest)
		cur = parent
	}
}
