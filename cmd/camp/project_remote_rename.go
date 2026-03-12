//go:build dev

package main

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteRenameCmd = &cobra.Command{
	Use:   "rename <old> <new>",
	Short: "Rename a remote in the project",
	Long: `Rename a git remote in the project repository.

Renaming to or from "origin" is allowed but triggers a warning because
origin is the conventional remote name used by submodule tracking. If
you need to change the URL instead, use "camp project remote set-url".

Examples:
  camp project remote rename upstream fork
  camp project remote rename origin old-origin   # warns, then renames`,
	Args: cobra.ExactArgs(2),
	RunE: runProjectRemoteRename,
}

func init() {
	projectRemoteCmd.AddCommand(projectRemoteRenameCmd)
}

func runProjectRemoteRename(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	oldName := args[0]
	newName := args[1]

	if oldName == "origin" {
		fmt.Printf("%s Renaming %s away from the canonical remote name.\n",
			ui.WarningIcon(), ui.Value("origin"))
		fmt.Println(ui.Dim("  Submodule tracking and fetch/push defaults may break."))
		fmt.Println()
	} else if newName == "origin" {
		fmt.Printf("%s Renaming %s to %s — this remote will become the canonical name.\n",
			ui.WarningIcon(), ui.Value(oldName), ui.Value("origin"))
		fmt.Println()
	}

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}

	if err := git.RenameRemote(ctx, resolved.Path, oldName, newName); err != nil {
		return fmt.Errorf("rename remote: %w", err)
	}

	fmt.Printf("%s Renamed remote %s → %s in project %s\n",
		ui.SuccessIcon(), ui.Value(oldName), ui.Value(newName), ui.Value(resolved.Name))

	return nil
}
