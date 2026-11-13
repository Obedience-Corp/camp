package prune

import (
	"context"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// deleteRemoteBranches handles confirmation and deletion of merged branches on origin.
func deleteRemoteBranches(ctx context.Context, path string, merged []string, opts Options, pr *ProjectResult) {
	if !opts.RemoteDelete || len(merged) == 0 {
		return
	}

	if opts.DryRun {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusWouldDelete,
				Detail: "remote",
			})
		}
		return
	}

	// Always confirm remote deletion independently — --force only covers local
	fmt.Printf("\n%s Will DELETE %d branch(es) from origin (irreversible):\n",
		ui.WarningIcon(), len(merged))
	for _, b := range merged {
		fmt.Printf("  %s origin/%s\n", ui.Dim("-"), b)
	}
	fmt.Print("\nDelete from remote? [y/N] ")
	var answer string
	// An empty answer is an error to Scanln and means no.
	_, _ = fmt.Scanln(&answer)
	if !strings.HasPrefix(strings.ToLower(answer), "y") {
		for _, branch := range merged {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusSkipped,
				Detail: "remote deletion cancelled",
			})
		}
		return
	}

	for _, branch := range merged {
		if err := git.DeleteRemoteBranch(ctx, path, branch); err != nil {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusError,
				Detail: err.Error(),
			})
		} else {
			pr.Results = append(pr.Results, Result{
				Branch: "origin/" + branch,
				Status: StatusDeleted,
				Detail: "remote",
			})
		}
	}
}

// pruneTrackingRefs handles pruning of stale remote tracking refs.
func pruneTrackingRefs(ctx context.Context, path string, opts Options, pr *ProjectResult) {
	if !opts.Remote {
		return
	}

	if opts.DryRun {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusWouldPrune,
		})
		return
	}

	count, err := git.PruneRemote(ctx, path)
	if err != nil {
		pr.Results = append(pr.Results, Result{
			Branch: "(remote tracking refs)",
			Status: StatusError,
			Detail: err.Error(),
		})
	} else {
		pr.Pruned = count
	}
}
