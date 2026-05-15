package main

import (
	"context"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/spf13/cobra"
)

var stageCmd = &cobra.Command{
	Use:   "stage",
	Short: "Stage changes in the campaign root",
	Long: `Stage changes in the campaign root directory without committing.

Runs the same auto-staging logic as 'camp commit' (including stale lock
file cleanup) but stops before creating a commit, so you can use a
different commit strategy (interactive 'git commit --patch', a GUI
client, signing flow, etc.).

At the campaign root, submodule ref changes (projects/*) are excluded
from staging by default to prevent accidental ref conflicts across
machines. Use --include-refs to stage them explicitly.

Use --sub to stage in the submodule detected from your current directory.
Use -p/--project to stage in a specific project (e.g., -p projects/camp).

Examples:
  camp stage
  camp stage --include-refs
  camp stage --sub
  camp stage -p projects/camp`,
	RunE: runStage,
}

var (
	stageSub         bool
	stageProject     string
	stageIncludeRefs bool
)

func init() {
	stageCmd.Flags().BoolVar(&stageSub, "sub", false, "Operate on the submodule detected from current directory")
	stageCmd.Flags().StringVarP(&stageProject, "project", "p", "", "Operate on a specific project/submodule path")
	stageCmd.Flags().BoolVar(&stageIncludeRefs, "include-refs", false, "Include submodule ref changes when staging at campaign root")

	rootCmd.AddCommand(stageCmd)
	stageCmd.GroupID = "git"

	if err := stageCmd.RegisterFlagCompletionFunc("project", completeProjectFlag); err != nil {
		panic(err)
	}
}

func runStage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	target, err := git.ResolveTarget(ctx, campRoot, stageSub, stageProject)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve target")
	}

	if target.IsSubmodule {
		fmt.Println(ui.Info(fmt.Sprintf("Operating on submodule: %s", target.Name)))
	}

	executor, err := git.NewExecutor(target.Path)
	if err != nil {
		return camperrors.Wrap(err, "failed to initialize git")
	}

	fmt.Println(ui.Info("Staging changes..."))
	refsExcluded := !target.IsSubmodule && !stageIncludeRefs
	if refsExcluded {
		paths, pathErr := git.ListSubmodulePaths(ctx, target.Path)
		if pathErr != nil {
			return pathErr
		}
		if err := git.StageAllExcluding(ctx, target.Path, paths); err != nil {
			return err
		}
	} else {
		if err := executor.StageAll(ctx); err != nil {
			return err
		}
	}

	staged, err := git.HasStagedChanges(ctx, target.Path)
	if err != nil {
		return err
	}
	if !staged {
		if refsExcluded {
			hasAny, hErr := executor.HasChanges(ctx)
			if hErr != nil {
				return hErr
			}
			if hasAny {
				fmt.Println(ui.Success("Nothing staged. Submodule ref changes were excluded; rerun with --include-refs to stage them."))
				return nil
			}
		}
		fmt.Println(ui.Success("Nothing to stage, working tree clean"))
		return nil
	}

	cmdutil.ShowStagedSummary(ctx, target.Path)
	fmt.Println(ui.Success("Changes staged"))
	fmt.Println(ui.Dim("Run 'camp commit' to record them."))
	return nil
}
