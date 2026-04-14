package project

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a project from campaign",
	Long: `Remove a project from the campaign.

By default, this only removes the project from git submodule tracking.
The project files remain in place for you to handle manually.

For linked projects, prefer 'camp project unlink'. Linked projects are
machine-local symlinks and are never deleted through this command.

Use --delete to also remove all project files. This is destructive
and requires confirmation unless --force is also specified.

Examples:
  camp project remove api-service           # Unregister submodule only
  camp project remove api-service --delete  # Also delete files (confirms)
  camp project remove api-service --delete --force  # Delete without confirmation
  camp project remove api-service --dry-run # Show what would be done`,
	Aliases: []string{"rm"},
	Args:    cobra.ExactArgs(1),
	RunE:    runProjectRemove,
}

func init() {
	Cmd.AddCommand(projectRemoveCmd)

	projectRemoveCmd.Flags().BoolP("delete", "d", false, "Also delete project files (destructive)")
	projectRemoveCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompts")
	projectRemoveCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
	projectRemoveCmd.Flags().Bool("no-commit", false, "Skip automatic git commit")
}

func runProjectRemove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := strings.TrimPrefix(args[0], "projects/")

	delete, _ := cmd.Flags().GetBool("delete")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Confirm if deleting and not forced
	if delete && !force && !dryRun {
		fmt.Printf("%s Delete project %s and all its files? This cannot be undone. [y/N] ",
			ui.WarningIcon(), ui.Value(name))
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println(ui.Dim("Aborted."))
			return nil
		}
	}

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	opts := projectsvc.RemoveOptions{
		Delete: delete,
		Force:  force,
		DryRun: dryRun,
	}

	result, err := projectsvc.Remove(ctx, root, name, opts)
	if err != nil {
		return err
	}

	// Print results
	if dryRun {
		fmt.Println(ui.Warning("Dry run - would remove:"))
		fmt.Println()
		fmt.Println(ui.KeyValue("  Project:", result.Name))
		if result.LinkRemoved {
			fmt.Printf("    %s Unlink linked project\n", ui.BulletIcon())
		}
		if result.SubmoduleRemoved {
			fmt.Printf("    %s Remove from git submodule tracking\n", ui.BulletIcon())
		}
		if result.FilesDeleted {
			fmt.Printf("    %s Delete files at %s\n", ui.BulletIcon(), ui.Dim(result.Path))
		}
		if result.WorktreeDeleted {
			fmt.Printf("    %s Delete worktrees for %s\n", ui.BulletIcon(), ui.Dim(result.Name))
		}
		return nil
	}

	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Removed project: "+result.Name))
	if result.LinkRemoved {
		fmt.Printf("  %s Linked project unlinked\n", ui.SuccessIcon())
	}
	if result.SubmoduleRemoved {
		fmt.Printf("  %s Submodule unregistered\n", ui.SuccessIcon())
	}
	if result.FilesDeleted {
		fmt.Printf("  %s Files deleted\n", ui.SuccessIcon())
	}
	if result.WorktreeDeleted {
		fmt.Printf("  %s Worktrees deleted\n", ui.SuccessIcon())
	}

	// Auto-commit structural campaign changes unless disabled.
	if !noCommit && !dryRun && (result.SubmoduleRemoved || result.LinkRemoved) {
		cfg, _ := config.LoadCampaignConfig(ctx, root)
		campaignID := ""
		if cfg != nil {
			campaignID = cfg.ID
		}
		files := commit.NormalizeFiles(root, result.Path)
		action := commit.ProjectUnlink
		if result.SubmoduleRemoved {
			files = commit.NormalizeFiles(root, ".gitmodules", result.Path)
			action = commit.ProjectRemove
		}
		commitResult := commit.Project(ctx, commit.ProjectOptions{
			Options: commit.Options{
				CampaignRoot:  root,
				CampaignID:    campaignID,
				Files:         files,
				SelectiveOnly: true,
			},
			Action:      action,
			ProjectName: result.Name,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
