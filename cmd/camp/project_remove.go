package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/project"
	"github.com/spf13/cobra"
)

var projectRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a project from campaign",
	Long: `Remove a project from the campaign.

By default, this only removes the project from git submodule tracking.
The project files remain in place for you to handle manually.

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
	projectCmd.AddCommand(projectRemoveCmd)

	projectRemoveCmd.Flags().BoolP("delete", "d", false, "Also delete project files (destructive)")
	projectRemoveCmd.Flags().BoolP("force", "f", false, "Skip confirmation prompts")
	projectRemoveCmd.Flags().Bool("dry-run", false, "Show what would be done without making changes")
}

func runProjectRemove(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	delete, _ := cmd.Flags().GetBool("delete")
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	// Confirm if deleting and not forced
	if delete && !force && !dryRun {
		fmt.Printf("Delete project '%s' and all its files? This cannot be undone. [y/N] ", name)
		reader := bufio.NewReader(os.Stdin)
		response, _ := reader.ReadString('\n')
		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	opts := project.RemoveOptions{
		Delete: delete,
		Force:  force,
		DryRun: dryRun,
	}

	result, err := project.Remove(ctx, root, name, opts)
	if err != nil {
		return err
	}

	// Print results
	if dryRun {
		fmt.Println("Dry run - would remove:")
		fmt.Printf("  Project: %s\n", result.Name)
		if result.SubmoduleRemoved {
			fmt.Println("  - Remove from git submodule tracking")
		}
		if result.FilesDeleted {
			fmt.Printf("  - Delete files at %s\n", result.Path)
		}
		if result.WorktreeDeleted {
			fmt.Printf("  - Delete worktrees for %s\n", result.Name)
		}
		return nil
	}

	fmt.Printf("Removed project: %s\n", result.Name)
	if result.SubmoduleRemoved {
		fmt.Println("  Submodule unregistered")
	}
	if result.FilesDeleted {
		fmt.Println("  Files deleted")
	}
	if result.WorktreeDeleted {
		fmt.Println("  Worktrees deleted")
	}

	return nil
}
