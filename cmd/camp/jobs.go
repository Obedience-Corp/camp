package main

import (
	"encoding/json"
	"fmt"
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
"throw away my work".`,
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
}

func init() {
	// The detached child inherits no useful working directory, so it is told
	// which campaign to serve rather than detecting one.
	jobsRunCmd.Flags().StringVar(&jobsOpts.campaign, "campaign", "",
		"Campaign root to serve (defaults to the detected campaign)")
	jobsCmd.Flags().BoolVar(&jobsOpts.json, "json", false,
		"Emit a structured JSON result")

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
// which job, where, how old, how many tries left, and whether anyone is on it.
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
	Summary  string `json:"summary"`
}

func runJobsList(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	entries, err := jobs.Snapshot(campRoot)
	if err != nil {
		return err
	}

	if jobsOpts.json {
		return emitJobsJSON(cmd, campRoot, entries)
	}
	renderJobsHuman(cmd, entries)
	return nil
}

func emitJobsJSON(cmd *cobra.Command, campRoot string, entries []jobs.Entry) error {
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
			ID:       e.ID,
			Seq:      e.Seq,
			State:    e.State,
			Lane:     e.Lane,
			Kind:     string(e.Kind),
			Class:    class,
			AgeMs:    e.Age(now).Milliseconds(),
			Attempts: e.Attempts,
			Stuck:    e.Stuck,
			Summary:  jobs.Describe(e.Job),
		})
	}

	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func renderJobsHuman(cmd *cobra.Command, entries []jobs.Entry) {
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
		state := e.State
		if e.Stuck {
			state = "stuck"
		}
		what := jobs.Describe(e.Job)
		if note := jobs.AttemptNote(e.Attempts, e.State == "failed"); note != "" {
			what = fmt.Sprintf("%s (%s)", what, note)
		}
		fmt.Fprintf(&b, "%-9s %-25s %-22s %-14s %6s  %s\n",
			state, e.ID, e.Lane, e.Kind, shortDuration(e.Age(now)), what)
	}
	_, _ = fmt.Fprint(out, b.String())

	// Every state that needs a decision names the command that makes it, so
	// the listing is actionable rather than merely informative.
	failed, stuck := 0, 0
	for _, e := range entries {
		switch {
		case e.State == "failed":
			failed++
		case e.Stuck:
			stuck++
		}
	}
	// The blank separator is its own Fprintln rather than a "\n" inside Dim,
	// which would style the newline and emit a line of trailing spaces.
	if failed > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Retry them with 'camp jobs retry all', or give up with 'camp jobs drop <id>'."))
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Dropping keeps the files on disk; your next commit picks them up."))
	}
	if stuck > 0 {
		_, _ = fmt.Fprintln(out)
		_, _ = fmt.Fprintln(out, ui.Dim(
			"Stuck jobs have no live worker. 'camp jobs run' serves them now."))
	}
}

// shortDuration renders an age in the largest unit that stays readable.
func shortDuration(d time.Duration) string {
	switch {
	case d <= 0:
		return "-"
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
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

	dropped, err := jobs.Drop(ctx, campRoot, args[0])
	if err != nil {
		return err
	}
	out := cmd.OutOrStdout()
	if len(dropped) == 0 {
		_, _ = fmt.Fprintln(out, ui.Dim("No failed jobs to drop."))
		return nil
	}

	// Say what survived, every time. Camp discarding something on the user's
	// instruction has to be explicit about what it did not discard, or "drop"
	// reads as "delete my work".
	_, _ = fmt.Fprintln(out, ui.Success(fmt.Sprintf("Dropped %s.", jobCountPhrase(len(dropped)))))
	_, _ = fmt.Fprintln(out, ui.Dim(
		"Their files are still in your working tree; your next commit picks them up."))
	return nil
}

func runJobsDrain(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	waited, err := drain.AllLanes(ctx, campRoot, drain.Write)
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
