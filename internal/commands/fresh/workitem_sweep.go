package fresh

import (
	"context"
	"fmt"
	"os"

	"github.com/Obedience-Corp/camp/internal/commands/workitem"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// runCampaignWorkitemSweep runs the tier-1 workitem sweep exactly once per camp
// fresh invocation, at the campaign root. It is called from the top-level fresh
// handlers (single-project RunE and runFreshBatch), never from executeFresh,
// which stays project-scoped: the sweep operates on workflow/<type>/ at the
// campaign root and has nothing to do with any one project's prune result.
//
// It honors completed_runs: "off" does no discovery at all; "report" prints the
// read-only banner; "sweep" (default) promotes eligible items. A dry-run fresh
// downgrades to "report" so it never mutates. A sweep failure is reported but
// never fails the fresh run, matching how executeFresh failures are surfaced in
// the batch summary without aborting the command; hence this returns nothing.
func runCampaignWorkitemSweep(ctx context.Context, cfg *config.FreshConfig, dryRun bool) {
	mode := cfg.ResolveFreshCompletedRuns()
	if mode == "off" {
		return
	}
	if dryRun {
		mode = workitem.FreshSweepModeReport
	}
	if err := workitem.RunFreshSweep(ctx, os.Stdout, mode); err != nil {
		fmt.Fprintf(os.Stderr, "%s workitem sweep reported an issue (fresh continues): %v\n", ui.WarningIcon(), err)
	}
}
