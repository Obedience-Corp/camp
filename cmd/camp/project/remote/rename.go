package remote

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a remote in the project",
	Long: `Rename a git remote in the project repository.

Renaming away from "origin" is blocked by default for submodule projects
because submodule tracking depends on the "origin" remote name. A future
"git submodule sync" would recreate origin from .gitmodules, undoing the
rename and leaving the project in a confusing state.

Use --force to override this guard. If you need to change the URL instead,
use "camp project remote set-url".

Renaming TO "origin" is allowed and will update .gitmodules to use the
new remote's URL as the canonical submodule URL.

Examples:
  camp project remote rename upstream fork
  camp project remote rename origin old-origin --force`,
	Args: cobra.ExactArgs(2),
	RunE: runProjectRemoteRename,
}

func init() {
	Cmd.AddCommand(projectRemoteRenameCmd)

	projectRemoteRenameCmd.Flags().BoolP("force", "f", false,
		"Allow renaming away from origin (submodule tracking may break)")
}

func runProjectRemoteRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	oldName := args[0]
	newName := args[1]
	force, _ := cmd.Flags().GetBool("force")

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}
	if err := resolved.RequireGit("git remotes"); err != nil {
		return err
	}

	isSubmodule := resolved.Source == project.SourceSubmodule
	submodulePath := resolved.LogicalPath

	// Guard: renaming away from origin on submodule projects
	if oldName == "origin" && isSubmodule && !force {
		fmt.Printf("%s Refusing to rename %s in a submodule project.\n",
			ui.WarningIcon(), ui.Value("origin"))
		fmt.Println(ui.Dim("  Submodule tracking depends on the 'origin' remote name."))
		fmt.Println(ui.Dim("  A future 'git submodule sync' would recreate origin, undoing this rename."))
		fmt.Println(ui.Dim("  To change the URL instead: camp project remote set-url <url>"))
		fmt.Println(ui.Dim("  To rename anyway:          camp project remote rename origin <new> --force"))
		return camperrors.Wrap(camperrors.ErrInvalidInput, "use --force to rename origin in submodule project")
	}

	if oldName == "origin" && !isSubmodule {
		fmt.Printf("%s Renaming %s away from the canonical remote name.\n",
			ui.WarningIcon(), ui.Value("origin"))
		fmt.Println(ui.Dim("  fetch/push defaults may break."))
		fmt.Println()
	}

	// Capture the URL of the source remote before renaming (for campaign-sync)
	var sourceURL string
	if isSubmodule && newName == "origin" {
		remotes, _ := git.ListRemotes(ctx, resolved.Path)
		for _, r := range remotes {
			if r.Name == oldName {
				sourceURL = r.FetchURL
				break
			}
		}
	}

	if err := git.RenameRemote(ctx, resolved.Path, oldName, newName); err != nil {
		return camperrors.Wrap(err, "rename remote")
	}

	fmt.Printf("%s Renamed remote %s → %s in project %s\n",
		ui.SuccessIcon(), ui.Value(oldName), ui.Value(newName), ui.Value(resolved.Name))

	// Campaign-sync: update .gitmodules when renaming TO origin (new canonical)
	if isSubmodule && newName == "origin" && sourceURL != "" {
		if err := git.SetDeclaredURL(ctx, campRoot, submodulePath, sourceURL); err != nil {
			fmt.Printf("%s Could not update .gitmodules: %s\n",
				ui.WarningIcon(), ui.Dim(err.Error()))
		} else {
			fmt.Printf("%s Updated .gitmodules to use %s as canonical URL\n",
				ui.SuccessIcon(), ui.Dim(sourceURL))

			syncErr := git.WithLockRetry(ctx, campRoot, git.SubmoduleRetryConfig(), func() error {
				return git.SyncSubmodule(ctx, campRoot, submodulePath)
			})
			if syncErr != nil {
				fmt.Printf("%s Could not sync submodule config: %s\n",
					ui.WarningIcon(), ui.Dim(syncErr.Error()))
			}

			stageErr := git.WithLockRetry(ctx, campRoot, git.DefaultRetryConfig(), func() error {
				return git.StageFiles(ctx, campRoot, ".gitmodules")
			})
			if stageErr != nil {
				fmt.Printf("%s Could not stage .gitmodules: %s\n",
					ui.WarningIcon(), ui.Dim(stageErr.Error()))
			} else {
				fmt.Printf("%s Staged .gitmodules\n", ui.SuccessIcon())
			}
		}
	}

	// Campaign-sync: warn when renaming away from origin on a submodule (forced)
	if isSubmodule && oldName == "origin" && force {
		fmt.Printf("\n%s Origin renamed away — submodule tracking is now broken.\n", ui.WarningIcon())
		fmt.Printf("  %s To fix, rename back or set a new origin:\n", ui.Dim("→"))
		fmt.Printf("    camp project remote rename %s origin --project %s\n", newName, resolved.Name)
		fmt.Printf("    camp project remote set-url <url> --project %s\n", resolved.Name)
	}

	return nil
}
