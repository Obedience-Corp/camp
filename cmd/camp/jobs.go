package main

import (
	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jobs"
	"github.com/spf13/cobra"
)

var jobsCmd = &cobra.Command{
	Use:   "jobs",
	Short: "Inspect and run camp's deferred commit queue",
	Long: `Inspect and run the deferred commit queue.

Camp defers its own bookkeeping commits so they do not hold your terminal. The
queue lives under .campaign/cache/jobs and is machine-local and disposable:
git is the record, this is only the work still on its way there.

Workers normally start themselves when a command enqueues work, so 'jobs run'
is mostly for debugging and for the detached child camp spawns.`,
}

var jobsRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Serve every lane with pending work, then exit",
	Long: `Serve every lane with pending work and exit when none is left.

This is the entrypoint camp spawns detached after enqueuing. Running it by
hand is safe: lanes are locked per repo, so a second worker simply finds the
lanes taken and exits.`,
	Args: cobra.NoArgs,
	RunE: runJobsRun,
}

var jobsOpts struct {
	campaign string
}

func init() {
	// The detached child inherits no useful working directory, so it is told
	// which campaign to serve rather than detecting one.
	jobsRunCmd.Flags().StringVar(&jobsOpts.campaign, "campaign", "",
		"Campaign root to serve (defaults to the detected campaign)")

	jobsCmd.AddCommand(jobsRunCmd)
	rootCmd.AddCommand(jobsCmd)
}

func runJobsRun(cmd *cobra.Command, args []string) error {
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
	// worker itself; `camp jobs list` is the surface for looking.
	return jobs.Run(ctx, campRoot)
}
