package main

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectRemoteAddCmd = &cobra.Command{
	Use:   "add <name> <url>",
	Short: "Add a new remote to the project",
	Long: `Add a new git remote to the project repository.

This does NOT modify .gitmodules — use set-url to change the canonical
origin for a submodule. Use this command to add secondary remotes such
as an upstream fork or a mirror.

After adding, a git fetch is performed to verify connectivity and
report how many refs are available.

Examples:
  camp project remote add upstream git@github.com:org/upstream.git
  camp project remote add mirror https://gitlab.com/org/repo.git`,
	Args: cobra.ExactArgs(2),
	RunE: runProjectRemoteAdd,
}

func init() {
	projectRemoteCmd.AddCommand(projectRemoteAddCmd)
}

func runProjectRemoteAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	remoteName := args[0]
	remoteURL := args[1]

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	resolved, err := project.Resolve(ctx, campRoot, flagRemoteProject)
	if err != nil {
		return err
	}

	if err := git.AddRemote(ctx, resolved.Path, remoteName, remoteURL); err != nil {
		return fmt.Errorf("add remote: %w", err)
	}

	fmt.Printf("%s Added remote %s → %s\n",
		ui.SuccessIcon(), ui.Value(remoteName), ui.Dim(remoteURL))

	// Fetch to verify and count branches
	fmt.Printf("  %s Fetching from %s...\n", ui.BulletIcon(), ui.Value(remoteName))

	fetchCmd := exec.CommandContext(ctx, "git", "-C", resolved.Path, "fetch", remoteName)
	fetchOut, fetchErr := fetchCmd.CombinedOutput()

	if fetchErr != nil {
		fmt.Printf("  %s Fetch failed: %s\n", ui.WarningIcon(), ui.Dim(strings.TrimSpace(string(fetchOut))))
		fmt.Printf("  %s Remote added but could not verify connectivity\n", ui.WarningIcon())
		return nil
	}

	// Count branches available under this remote
	lsCmd := exec.CommandContext(ctx, "git", "-C", resolved.Path,
		"branch", "-r", "--list", remoteName+"/*")
	lsOut, _ := lsCmd.Output()

	count := 0
	for _, line := range strings.Split(string(lsOut), "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}

	if count > 0 {
		fmt.Printf("  %s Fetched %s %s\n",
			ui.SuccessIcon(), ui.Value(fmt.Sprintf("%d", count)),
			pluralize(count, "branch", "branches"))
	} else {
		fmt.Printf("  %s Fetch succeeded (empty repository)\n", ui.SuccessIcon())
	}

	return nil
}

