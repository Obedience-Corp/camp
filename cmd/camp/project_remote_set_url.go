//go:build dev

package main

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteSetURLCmd = &cobra.Command{
	Use:   "set-url <url>",
	Short: "Update a remote URL for the project",
	Long: `Atomically update a remote URL across all three locations:
  1. .gitmodules  (canonical, tracked in git)
  2. local git submodule config (.git/config of the campaign root)
  3. remote config inside the project repo

Only relevant for submodule projects. For non-submodule projects,
only the remote config inside the project repo is updated.

Flags:
  --name      Remote name to update (default: origin)
  --no-verify Skip connectivity check after updating
  --no-stage  Skip auto-staging .gitmodules

Examples:
  camp project remote set-url git@github.com:org/new-name.git
  camp project remote set-url https://github.com/org/repo.git --name upstream
  camp project remote set-url git@github.com:org/repo.git --no-verify`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectRemoteSetURL,
}

func init() {
	projectRemoteCmd.AddCommand(projectRemoteSetURLCmd)

	projectRemoteSetURLCmd.Flags().StringP("name", "n", "origin", "Remote name to update")
	projectRemoteSetURLCmd.Flags().Bool("no-verify", false, "Skip connectivity check after updating")
	projectRemoteSetURLCmd.Flags().Bool("no-stage", false, "Skip auto-staging .gitmodules")
}

func runProjectRemoteSetURL(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	newURL := args[0]

	remoteName, _ := cmd.Flags().GetString("name")
	noVerify, _ := cmd.Flags().GetBool("no-verify")
	noStage, _ := cmd.Flags().GetBool("no-stage")

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}

	isSubmodule, _ := git.IsSubmodule(resolved.Path)
	submodulePath := strings.TrimPrefix(resolved.Path, campRoot+"/")

	// Show BEFORE state
	fmt.Printf("Updating remote %s for project %s\n\n",
		ui.Value(remoteName), ui.Value(resolved.Name))

	if isSubmodule {
		declaredBefore, _ := git.GetDeclaredURL(ctx, campRoot, submodulePath)
		if declaredBefore != "" {
			fmt.Printf("  %s %s\n", ui.Dim("before (.gitmodules):"), ui.Dim(declaredBefore))
		}
	}

	remotesBefore, _ := git.ListRemotes(ctx, resolved.Path)
	for _, r := range remotesBefore {
		if r.Name == remoteName {
			fmt.Printf("  %s %s\n", ui.Dim("before (remote):      "), ui.Dim(r.FetchURL))
			break
		}
	}
	fmt.Println()

	// Step 1: Update .gitmodules (submodule only)
	if isSubmodule {
		if err := git.SetDeclaredURL(ctx, campRoot, submodulePath, newURL); err != nil {
			return fmt.Errorf("update .gitmodules: %w", err)
		}
		fmt.Printf("  %s Updated .gitmodules\n", ui.SuccessIcon())
	}

	// Step 2: Sync submodule config (submodule only)
	if isSubmodule {
		if err := git.SyncSubmodule(ctx, campRoot, submodulePath); err != nil {
			return fmt.Errorf("sync submodule config: %w", err)
		}
		fmt.Printf("  %s Synced local submodule config\n", ui.SuccessIcon())
	}

	// Step 3: Update remote URL inside the project repo
	if err := git.SetRemoteURL(ctx, resolved.Path, remoteName, newURL); err != nil {
		return fmt.Errorf("set remote URL in project: %w", err)
	}
	fmt.Printf("  %s Set remote %s URL\n", ui.SuccessIcon(), ui.Value(remoteName))

	// Step 4: Verify connectivity
	if !noVerify {
		if err := git.VerifyRemote(ctx, resolved.Path, remoteName); err != nil {
			fmt.Printf("  %s Connectivity check failed: %s\n", ui.WarningIcon(), ui.Dim(err.Error()))
			fmt.Printf("  %s URL was updated but remote may not be reachable\n", ui.WarningIcon())
		} else {
			fmt.Printf("  %s Remote verified reachable\n", ui.SuccessIcon())
		}
	}

	// Step 5: Auto-stage .gitmodules
	if isSubmodule && !noStage {
		if err := git.StageFiles(ctx, campRoot, ".gitmodules"); err != nil {
			fmt.Printf("  %s Could not stage .gitmodules: %s\n", ui.WarningIcon(), ui.Dim(err.Error()))
		} else {
			fmt.Printf("  %s Staged .gitmodules\n", ui.SuccessIcon())
		}
	}

	// Show AFTER state
	fmt.Println()
	fmt.Printf("%s Remote URL updated:\n", ui.SuccessIcon())
	fmt.Printf("  %s\n", ui.Value(newURL))

	return nil
}
