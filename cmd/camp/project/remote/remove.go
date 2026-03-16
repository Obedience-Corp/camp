package remote

import (
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteRemoveCmd = &cobra.Command{
	Use:     "remove <name>",
	Short:   "Remove a remote from the project",
	Aliases: []string{"rm"},
	Long: `Remove a git remote from the project repository.

Removing the "origin" remote is blocked by default because it is the
canonical remote for submodule tracking. Use --force to override.

When --force is used to remove origin from a submodule project, the
.gitmodules entry is also cleaned up to keep the campaign consistent.

Note: if you want to change the canonical URL instead of removing it,
use "camp project remote set-url".

Examples:
  camp project remote remove upstream
  camp project remote remove origin --force   # also cleans .gitmodules`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectRemoteRemove,
}

func init() {
	Cmd.AddCommand(projectRemoteRemoveCmd)

	projectRemoteRemoveCmd.Flags().BoolP("force", "f", false,
		"Allow removing the origin remote (dangerous)")
}

func runProjectRemoteRemove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	remoteName := args[0]

	force, _ := cmd.Flags().GetBool("force")

	if remoteName == "origin" && !force {
		fmt.Printf("%s Refusing to remove %s remote.\n",
			ui.WarningIcon(), ui.Value("origin"))
		fmt.Println(ui.Dim("  origin is the canonical remote for submodule tracking."))
		fmt.Println(ui.Dim("  To change its URL, use: camp project remote set-url <url>"))
		fmt.Println(ui.Dim("  To remove it anyway:    camp project remote remove origin --force"))
		return fmt.Errorf("use --force to remove origin")
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}

	if err := git.RemoveRemote(ctx, resolved.Path, remoteName); err != nil {
		return fmt.Errorf("remove remote: %w", err)
	}

	fmt.Printf("%s Removed remote %s from project %s\n",
		ui.SuccessIcon(), ui.Value(remoteName), ui.Value(resolved.Name))

	// Campaign-sync: if we just removed origin from a submodule, clean up .gitmodules
	if remoteName == "origin" {
		isSubmodule, _ := git.IsSubmodule(resolved.Path)
		if isSubmodule {
			submodulePath := strings.TrimPrefix(resolved.Path, campRoot+"/")
			if err := git.RemoveDeclaredSubmodule(ctx, campRoot, submodulePath); err != nil {
				fmt.Printf("%s Could not clean .gitmodules entry: %s\n",
					ui.WarningIcon(), ui.Dim(err.Error()))
				fmt.Printf("  %s Manual cleanup: git -C %s config -f .gitmodules --remove-section submodule.%s\n",
					ui.Dim("→"), campRoot, submodulePath)
			} else {
				fmt.Printf("%s Removed .gitmodules entry for %s\n",
					ui.SuccessIcon(), ui.Value(submodulePath))
			}

			// Stage the .gitmodules change
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

	return nil
}
