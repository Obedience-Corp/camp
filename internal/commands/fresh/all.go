package fresh

import (
	"fmt"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
)

// allNoDrain is `camp fresh all --no-drain`. Declared here rather than reused
// from the parent because a non-persistent parent flag is not visible on a
// subcommand, so the parent's flag could not be passed at all.
var allNoDrain bool

func newAllCommand(freshCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "all",
		Short: "Run fresh across all project submodules",
		Long: `Run the fresh cycle (fetch and safely sync default, prune, optional branch)
across every project submodule in the camp.

Examples:
  camp fresh all                     # Sync all projects
  camp fresh all --branch develop    # Sync all and create develop branch
  camp fresh all --dry-run           # Preview across all projects
  camp fresh all --no-prune          # Sync without pruning`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a camp")
			}

			paths, err := git.ListSubmodulePathsRecursive(ctx, campRoot, "projects/")
			if err != nil {
				return camperrors.Wrap(err, "failed to list submodules")
			}

			if len(paths) == 0 {
				fmt.Println(ui.Info("No submodules found in this camp"))
				return nil
			}

			// Load fresh config once
			cfg, err := config.LoadFreshConfig(ctx, campRoot)
			if err != nil {
				return camperrors.Wrap(err, "loading fresh config")
			}

			// Read persistent flags from parent (freshCmd)
			flags := getFreshFlagSet(freshCmd)

			targets := make([]freshTarget, 0, len(paths))
			for _, p := range paths {
				targets = append(targets, freshTarget{
					name: git.SubmoduleDisplayName(p),
					path: filepath.Join(campRoot, p),
				})
			}
			if err := drainFreshTargets(ctx, targets, allNoDrain, flags.dryRun); err != nil {
				return err
			}

			return runFreshBatch(ctx, cfg, targets, flags, "Running fresh across all projects...")
		},
	}
	cmd.Flags().BoolVar(&allNoDrain, "no-drain", false,
		"Do not wait for camp's queued commits first")
	return cmd
}
