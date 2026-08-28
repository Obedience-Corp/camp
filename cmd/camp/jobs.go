package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/drain"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// JobsJSONVersion is the schema of `camp jobs --json`.
const JobsJSONVersion = "jobs/v1alpha1"

var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Inspect and run camp's deferred commit queue",
	Long: `Inspect and run the deferred commit queue.

Camp defers its own bookkeeping commits so they do not hold your terminal. The
queue lives under .campaign/cache/jobs and is machine-local and disposable:
git is the record, this is only the work still on its way there.

Run bare to see what is queued, running, or failed. Nothing here is required in
normal use: workers start themselves, and every command that touches git
history waits for the queue before it runs.

Examples:
  camp jobs                    # what is queued, running, or failed
  camp jobs --json             # the same, for scripts and agents
  camp jobs retry all          # requeue everything that failed
  camp jobs drop <id>          # give up on one job, keeping its content
  camp jobs drain              # wait for every lane, then exit`,
	Args: cobra.NoArgs,
	RunE: jsoncontract.RunE(JobsJSONVersion, func() bool { return jobsOpts.json }, runJobsList),
}

var jobsRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Serve every lane with pending work, then exit",
	Long: `Serve every lane with pending work and exit when none is left.

This is both the entrypoint camp spawns detached after enqueuing work, and the
way to run the queue in the foreground when something looks wrong. Running it
by hand is safe at any time: lanes are locked per repo, so a second worker
finds the lanes taken and exits rather than duplicating anything.

It prints nothing on success. Per-job transitions go to
.campaign/cache/jobs/worker.log, which is where a detached worker's story is;
'camp jobs' is the surface for looking at what is still outstanding.`,
	Args: cobra.NoArgs,
	RunE: runJobsRun,
}

var jobsRetryCmd = &cobra.Command{
	Use:   "retry <id|all>",
	Short: "Requeue failed jobs",
	Long: `Move failed jobs back to pending and start a worker for them.

Attempts reset, because a retry is you deciding the cause is gone. Keeping the
count would let a job that failed three times for a reason you have since fixed
be parked again on its first new attempt.`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsRetry,
}

var jobsDropCmd = &cobra.Command{
	Use:   "drop <id|all>",
	Short: "Give up on failed jobs, keeping their content",
	Long: `Discard failed jobs without discarding what they were going to commit.

Only the job file is removed. The intent, manifest, or marker the job was going
to commit stays in your working tree, uncommitted, for the next ordinary commit
to pick up. Dropping a job means "stop trying to commit this for me", never
"throw away my work".

Failed jobs only, unless you pass --running. A running job has a worker on it,
so giving up means stopping that worker: --running sends it SIGTERM, waits for
it to stop, and drops the job. Use it for a job 'camp jobs' reports as stalled,
where a commit message writer has stopped answering and nothing else will end
the wait.

Stopping a worker returns the other jobs it was running to pending, so they are
served again by the next worker. Only the jobs you named are dropped.

Examples:
  camp jobs drop <id>              # a failed job
  camp jobs drop all               # every failed job
  camp jobs drop --running <id>    # a stalled job, and the worker holding it`,
	Args: cobra.ExactArgs(1),
	RunE: runJobsDrop,
}

var jobsDrainCmd = &cobra.Command{
	Use:   "drain",
	Short: "Wait until every lane is empty",
	Long: `Block until no queued commit is outstanding anywhere in the campaign.

Commands that touch git history already do this for the repo they act on, so
this is for the cases that are not one command: before archiving a machine,
before a manual git operation camp does not wrap, or to watch the queue finish.

Artifact manifest jobs are exempt here as everywhere: they carry the commit
they describe, so they are correct whenever they land.`,
	Args: cobra.NoArgs,
	RunE: runJobsDrain,
}

var jobsOpts struct {
	campaign string
	json     bool
	running  bool
}

func init() {
	// The detached child inherits no useful working directory, so it is told
	// which campaign to serve rather than detecting one.
	jobsRunCmd.Flags().StringVar(&jobsOpts.campaign, "campaign", "",
		"Campaign root to serve (defaults to the detected campaign)")
	jobsCmd.Flags().BoolVar(&jobsOpts.json, "json", false,
		"Emit a structured JSON result")
	jobsDropCmd.Flags().BoolVar(&jobsOpts.running, "running", false,
		"Also drop running jobs, stopping the worker on each one's lane")

	jobsCmd.AddCommand(jobsRunCmd, jobsRetryCmd, jobsDropCmd, jobsDrainCmd)
	rootCmd.AddCommand(jobsCmd)
	jobsCmd.GroupID = "git"
	jobsCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JobsJSONVersion, func() bool { return jobsOpts.json }))
}

// jobsPayload is the --json document.
type jobsPayload struct {
	SchemaVersion string `json:"schema_version"`
	CampaignRoot  string `json:"campaign_root"`
	Pending       int    `json:"pending"`
	Running       int    `json:"running"`
	Failed        int    `json:"failed"`
	// Jobs is always an array, never null, so a consumer can iterate without
	// a nil check on the empty queue that is the normal case.
	Jobs []jobJSON `json:"jobs"`
}

// jobJSON is one row. The fields are what a caller needs to decide what to do:
// which job, where, how old, how many attempts have started, and whether anyone is on it.
type jobJSON struct {
	ID       string `json:"id"`
	Seq      int    `json:"seq"`
	State    string `json:"state"`
	Lane     string `json:"lane"`
	Kind     string `json:"kind"`
	Class    string `json:"class"`
	AgeMs    int64  `json:"age_ms"`
	Attempts int    `json:"attempts"`
	Stuck    bool   `json:"stuck"`
	// RunningMs is how long the current attempt has been running, zero when
	// the job is not running. Age answers "how long ago was this asked for",
	// which a backed-up queue can make large with nothing wrong; this answers
	// "how long has this been in progress", which is the one that says whether
	// anything is happening.
	RunningMs int64 `json:"running_ms"`
	// Stalled marks a running job that needs a person: nobody is serving its
	// lane, or the attempt has outrun the writer's budget. A superset of
	// Stuck, which keeps its exact meaning so a consumer reading it does not
	// change behavior under this field.
	Stalled bool `json:"stalled"`
	// StalledReason says which, in the same words the table prints. Empty
	// unless Stalled.
	StalledReason string `json:"stalled_reason,omitempty"`
	// Superseded marks a failed job that retrying can never fix, because
	// history moved past the commit it was queued against. An agent reading
	// this queue needs it to pick 'drop' over 'retry' without first failing.
	Superseded bool   `json:"superseded"`
	Summary    string `json:"summary"`
	// LastError is why the most recent attempt failed, as the worker recorded
	// it when it parked the job. Empty for anything not parked, and prose
	// rather than a code: it is there to be read, not matched on.
	LastError string `json:"last_error,omitempty"`
}

func runJobsList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	entries, err := jobs.Snapshot(ctx, campRoot)
	if err != nil {
		return err
	}

	superseded := supersededIDs(ctx, campRoot, entries)
	if jobsOpts.json {
		return emitJobsJSON(cmd, campRoot, entries, superseded)
	}
	renderJobsHuman(cmd, entries, superseded)
	return nil
}

// supersededIDs marks the failed jobs a retry can never fix.
//
// Computed once for both renderers so the human listing and --json cannot
// disagree, and only for failed jobs, so a queue that is merely busy pays no
// git calls at all.
func supersededIDs(ctx context.Context, campRoot string, entries []jobs.Entry) map[string]bool {
	marked := map[string]bool{}
	for _, e := range entries {
		if jobs.Superseded(ctx, campRoot, e) {
			marked[e.ID] = true
		}
	}
	return marked
}

func emitJobsJSON(cmd *cobra.Command, campRoot string, entries []jobs.Entry, superseded map[string]bool) error {
	now := time.Now()
	payload := jobsPayload{
		SchemaVersion: JobsJSONVersion,
		CampaignRoot:  campRoot,
		Jobs:          make([]jobJSON, 0, len(entries)),
	}
	for _, e := range entries {
		switch e.State {
		case "pending":
			payload.Pending++
		case "running":
			payload.Running++
		case "failed":
			payload.Failed++
		}
		class := string(e.Class)
		if class == "" {
			class = string(jobs.ClassCommit)
		}
		payload.Jobs = append(payload.Jobs, jobJSON{
			ID:            e.ID,
			Seq:           e.Seq,
			State:         e.State,
			Lane:          e.Lane,
			Kind:          string(e.Kind),
			Class:         class,
			AgeMs:         e.Age(now).Milliseconds(),
			Attempts:      e.Attempts,
			Stuck:         e.Stuck,
			RunningMs:     e.RunningFor.Milliseconds(),
			Stalled:       e.Stalled,
			StalledReason: e.StalledReason,
			Superseded:    superseded[e.ID],
			Summary:       jobs.Describe(e.Job),
			LastError:     e.LastError,
		})
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

// jobsWhat renders the WHAT column: what the job is, then whatever about it
// needs a decision.
//
// A running job always reports how long its attempt has been going. Without it
// the listing describes a state and never a rate, and "running" reads the same
// at three seconds and at fifty minutes, which is precisely the confusion that
// let a wedged commit message writer look like ordinary progress.
func jobsWhat(e jobs.Entry, superseded bool) string {
	var notes []string
	switch {
	case e.Stalled:
		notes = append(notes, e.StalledReason)
	case e.State == "running":
		notes = append(notes, "running "+jobs.ShortDuration(e.RunningFor))
	}
	if note := jobs.AttemptNote(e.Attempts, e.State == "failed"); note != "" {
		notes = append(notes, note)
	}
	if superseded {
		// The row carries it too, not only the footer: with a mix of
		// retryable and superseded jobs the footer can say how many but not
		// which, and "which" is what the user has to act on.
		notes = append(notes, "cannot retry")
	}
	what := jobs.Describe(e.Job)
	if len(notes) == 0 {
		return what
	}
	return what + " (" + strings.Join(notes, ", ") + ")"
}

func renderJobsHuman(cmd *cobra.Command, entries []jobs.Entry, superseded map[string]bool) {
	out := cmd.OutOrStdout()
	if len(entries) == 0 {
		_, _ = fmt.Fprintln(out, ui.Dim("No deferred commits queued."))
		return
	}

	now := time.Now()
	var b strings.Builder
	// The id column fits a real job id exactly ("job-" + a 16-char UTC stamp +
	// "-" + 4 hex = 25), plus one space. A narrower column does not truncate,
	// it just runs into the next one.
	fmt.Fprintf(&b, "%-9s %-25s %-22s %-14s %6s  %s\n",
		"STATE", "ID", "LANE", "KIND", "AGE", "WHAT")
	for _, e := range entries {
		// One word for "this row is trouble". A running job whose lane has no
		// worker and one whose writer stopped answering are different
		// diagnoses with different fixes, but they are the same discovery, and
		// the reason beside them says which.
		state := e.State
		if e.Stalled {
			state = "stalled"
		}
		fmt.Fprintf(&b, "%-9s %-25s %-22s %-14s %6s  %s\n",
			state, e.ID, e.Lane, e.Kind, jobs.ShortDuration(e.Age(now)),
			jobsWhat(e, superseded[e.ID]))
		// The failure reason gets a line of its own rather than a longer WHAT.
		// The columns already reach past sixty characters, so a sentence
		// appended to the last one wraps in the middle of itself; indented
		// underneath, it stays readable in a narrow terminal, and it is the
		// one thing on the row the user cannot work out for themselves.
		if e.LastError != "" {
			fmt.Fprintf(&b, "%s\n", ui.Dim("  "+e.LastError))
		}
	}
	_, _ = fmt.Fprint(out, b.String())

	// Every state that needs a decision names the command that makes it, so
	// the listing is actionable rather than merely informative.
	failed, noWorker, overBudget, stale := 0, 0, 0, 0
	for _, e := range entries {
		switch {
		case e.State == "failed":
			failed++
			if superseded[e.ID] {
				stale++
			}
		case e.Stuck:
			noWorker++
		case e.Stalled:
			overBudget++
		}
	}
	// The blank separator is its own Fprintln rather than a "\n" inside Dim,
	// which would style the newline and emit a line of trailing spaces.
	if failed > 0 {
		_, _ = fmt.Fprintln(out)
		// Retry is only offered when retrying could actually work. A job
		// whose captured changes conflict with what landed fails identically
		// every time, so naming the command that does that would send the
		// user around a loop with no exit. A moved parent on its own is not
		// that case: the worker re-applies onto it.
		switch stale {
		case 0:
			_, _ = fmt.Fprintln(out, ui.Dim(
				"Retry them with 'camp jobs retry all', or give up with 'camp jobs drop <id>'."))
		case failed:
			_, _ = fmt.Fprintln(out, ui.Dim(
				"Retrying will not help: what landed since conflicts with what these were"))
			_, _ = fmt.Fprintln(out, ui.Dim(
				"queued to commit. Drop them with 'camp jobs drop <id>'."))
		default:
			_, _ = fmt.Fprintln(out, ui.Dim(
				"Retry them with 'camp jobs retry all', or give up with 'camp jobs drop <id>'."))
			_, _ = fmt.Fprintln(out, ui.Dim(fmt.Sprintf(
				"The %d marked 'cannot retry' will not come back: what landed since", stale)))
			_, _ = fmt.Fprintln(out, ui.Dim(
				"conflicts with what they were queued to commit. Drop those."))
		}
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Dropping keeps the files on disk; your next commit picks them up."))
	}
	if noWorker > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Stalled with no live worker: nothing is running them. 'camp jobs run' serves them now."))
	}
	// The other stall needs the opposite thing said. There is a worker, it is
	// heartbeating, and that is exactly why nothing will rescue this job: the
	// queue reads a held lane as work in progress, so it waits, and every
	// drain behind it waits too. Only a person can decide it is over.
	if overBudget > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Stalled past the writer budget: a worker is on it and has stopped making progress."))
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Give up with 'camp jobs drop --running <id>', which stops that worker too."))
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Dropping keeps the files on disk; your next commit picks them up."))
	}
}

func runJobsRun(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	campRoot := jobsOpts.campaign
	if campRoot == "" {
		detected, err := campaign.DetectCached(ctx)
		if err != nil {
			return camperrors.Wrap(err, "not in a campaign")
		}
		campRoot = detected
	}

	// Silent on success, including when there was nothing to do. This runs
	// detached far more often than interactively, with stdout wired to
	// worker.log, so anything printed here would accumulate as noise in the
	// one file that exists to explain real work. Transitions are logged by the
	// worker itself; `camp jobs` is the surface for looking.
	return jobs.Run(ctx, campRoot)
}

func runJobsRetry(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	requeued, err := jobs.Retry(ctx, campRoot, args[0])
	if err != nil {
		return err
	}
	if len(requeued) == 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("No failed jobs to retry."))
		return nil
	}

	// Spawn per lane, not per job: one worker drains a whole lane, and a
	// retried job with nothing serving it is exactly the state the user just
	// asked camp to leave.
	for _, lane := range distinctRepos(requeued) {
		jobs.SpawnIfNeeded(ctx, campRoot, lane)
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(),
		ui.Success(fmt.Sprintf("Requeued %s.", jobCountPhrase(len(requeued)))))
	return nil
}

func runJobsDrop(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}
	selector := args[0]
	out := cmd.OutOrStdout()

	if jobsOpts.running {
		return dropIncludingRunning(ctx, out, campRoot, selector)
	}

	dropped, err := jobs.Drop(ctx, campRoot, selector)
	if err != nil {
		return runningDropHint(campRoot, selector, err)
	}
	if len(dropped) == 0 {
		_, _ = fmt.Fprintln(out, ui.Dim("No failed jobs to drop."))
		return nil
	}
	reportDropped(out, len(dropped))
	return nil
}

// dropIncludingRunning drops the failed and running jobs a selector names.
//
// Failed first, because dropping one is a file removal and dropping a running
// one stops a process. If the selector matches nothing at all there is no
// reason to have signalled anybody, and doing the reversible half first keeps
// the irreversible half from happening on a typo.
func dropIncludingRunning(ctx context.Context, out io.Writer, campRoot, selector string) error {
	// A selector matching nothing in failed/ is deferred rather than returned:
	// with --running it may legitimately name a job that is running and not
	// failed, and "no failed job with that id" would be a wrong answer to a
	// right command. Anything else that went wrong is returned now, because a
	// job file camp could not remove is a problem in its own right.
	dropped, failedErr := jobs.Drop(ctx, campRoot, selector)
	var noMatch *jobs.NoMatchError
	if failedErr != nil && !errors.As(failedErr, &noMatch) {
		return failedErr
	}

	running, stops, err := jobs.DropRunning(ctx, campRoot, selector)
	dropped = append(dropped, running...)
	// Reported before any error, because a worker camp stopped stays stopped
	// whether or not the rest of the command succeeded.
	reportWorkerStops(out, stops)
	if err != nil {
		return err
	}

	if len(dropped) == 0 {
		if failedErr != nil {
			return camperrors.Newf("no failed or running job with id %q", selector)
		}
		_, _ = fmt.Fprintln(out, ui.Dim("No failed or running jobs to drop."))
		return nil
	}
	reportDropped(out, len(dropped))

	// The worker camp just stopped may have been serving jobs that had nothing
	// to do with this one, and stopping it returned them to pending. Leaving
	// them there would make "give up on this commit" quietly mean "and stall
	// the ones behind it", which is not what the user asked for.
	if blocking, err := jobs.OutstandingAll(campRoot); err == nil {
		jobs.EnsureServed(ctx, campRoot, blocking)
	}
	return nil
}

// reportDropped says what was dropped and, every time, what survived.
//
// Camp discarding something on the user's instruction has to be explicit about
// what it did not discard, or "drop" reads as "delete my work".
func reportDropped(out io.Writer, n int) {
	_, _ = fmt.Fprintln(out, ui.Success(fmt.Sprintf("Dropped %s.", jobCountPhrase(n))))
	_, _ = fmt.Fprintln(out, ui.Dim(
		"Their files are still in your working tree; your next commit picks them up."))
}

// runningDropHint turns "no failed job with that id" into the truth when the
// id names a job that is running.
//
// The plain refusal is accurate and useless: the user is looking at the job in
// 'camp jobs' and being told it does not exist. This says which state it is in
// and the flag that acts on it, in one line, because being one flag away from
// the thing you asked for should not require reading the help.
func runningDropHint(campRoot, selector string, dropErr error) error {
	entry, ok := jobs.RunningMatch(campRoot, selector)
	if !ok {
		return dropErr
	}
	return camperrors.Newf(
		"job %s is running, not failed; stop its worker and drop it with "+
			"'camp jobs drop --running %s'", entry.ID, selector)
}

// reportWorkerStops says which workers camp stopped.
//
// Never silent. Camp killed a process the user did not start, on their
// instruction but out of their sight, and a stop that leaves no trace is
// indistinguishable from a job that finished on its own.
func reportWorkerStops(out io.Writer, stops []jobs.WorkerStop) {
	for _, stop := range stops {
		switch {
		case stop.PID == 0:
			_, _ = fmt.Fprintln(out, ui.Dim(
				fmt.Sprintf("Lane %s had no live worker to stop.", stop.Lane)))
		case stop.Killed:
			// Worth its own wording: a killed worker never ran its cleanup, so
			// a message writer it had started may still be alive.
			_, _ = fmt.Fprintln(out, ui.Warning(fmt.Sprintf(
				"Worker %d on lane %s ignored the stop signal and was killed; "+
					"check for a commit message writer it left behind.",
				stop.PID, stop.Lane)))
		default:
			_, _ = fmt.Fprintln(out, ui.Success(fmt.Sprintf(
				"Stopped the worker on lane %s (pid %d).", stop.Lane, stop.PID)))
		}
	}
}

func runJobsDrain(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	waited, err := drain.AllLanes(ctx, campRoot, drain.Wait)
	if err != nil {
		return err
	}
	if waited > 0 {
		_, _ = fmt.Fprintln(cmd.OutOrStdout(),
			ui.Success(fmt.Sprintf("Queue drained in %s.", waited.Round(time.Millisecond))))
		return nil
	}
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("Nothing was queued."))
	return nil
}

// distinctRepos returns the lanes a set of jobs live in, each once.
func distinctRepos(list []jobs.Job) []string {
	seen := make(map[string]struct{}, len(list))
	var repos []string
	for _, job := range list {
		if _, ok := seen[job.Repo]; ok {
			continue
		}
		seen[job.Repo] = struct{}{}
		repos = append(repos, job.Repo)
	}
	return repos
}

func jobCountPhrase(n int) string {
	if n == 1 {
		return "1 job"
	}
	return fmt.Sprintf("%d jobs", n)
}

// maybeWarnFailedJobs is the hook every foreground command runs through.
//
// It sits in the root command's PersistentPreRun rather than in each command,
// so a command added later inherits the notice without anyone remembering to
// wire it. Three commands are excluded and one class of caller is:
//
//   - camp jobs and its subcommands, where the notice would restate what the
//     user is already looking at
//   - completion and shell-init, which produce machine input, and where an
//     extra line on stderr is a broken shell rather than a warning
//   - anything under --json, for the same reason
//
// It never errors. A notice that cannot be computed must not break the command
// it was going to annotate.
func maybeWarnFailedJobs(cmd *cobra.Command) {
	if cmd == nil || suppressesNotices(cmd) {
		return
	}
	campRoot, err := campaign.DetectCached(cmd.Context())
	if err != nil {
		return
	}
	renderFailedJobNotice(campRoot)
}

// suppressesNotices reports whether a command's output must stay clean.
func suppressesNotices(cmd *cobra.Command) bool {
	if f := cmd.Flags().Lookup("json"); f != nil && f.Changed {
		return true
	}
	for c := cmd; c != nil; c = c.Parent() {
		switch c.Name() {
		case "jobs", "complete", "completion", "shell-init", "__complete", "__completeNoDesc":
			return true
		}
	}
	return false
}

// renderFailedJobNotice prints one line when deferred commits have failed.
//
// It runs before every foreground command rather than once, and does not
// dismiss. A failed job is work camp promised to do and did not, so the user
// has to find out without going looking; a notice that appeared once and then
// stopped would be the same as no notice for anyone who was not watching that
// terminal. It is one terse line on purpose: it repeats often, and the detail
// is one command away.
func renderFailedJobNotice(campRoot string) {
	n := jobs.FailedCount(campRoot)
	if n == 0 {
		return
	}
	noun := "commits"
	if n == 1 {
		noun = "commit"
	}
	_, _ = fmt.Fprintln(os.Stderr, ui.Warning(
		fmt.Sprintf("! %d deferred %s failed (camp jobs)", n, noun)))
}
