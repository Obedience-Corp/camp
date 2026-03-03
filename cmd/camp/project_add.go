package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectAddCmd = &cobra.Command{
	Use:   "add <source>",
	Short: "Add a project to campaign",
	Long: `Add a git repository as a project in the campaign.

The project is cloned as a git submodule into the projects/ directory.
A worktree directory is also created for future parallel development.

Source can be:
  - SSH URL:   git@github.com:org/repo.git
  - HTTPS URL: https://github.com/org/repo.git
  - Local path (with --local): ./existing-repo

Examples:
  camp project add git@github.com:org/api.git           # Add remote repo
  camp project add https://github.com/org/web.git       # Add via HTTPS
  camp project add --local ./my-repo --name my-project  # Add existing local repo
  camp project add git@github.com:org/api.git --name backend  # Custom name`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectAdd,
}

func init() {
	projectCmd.AddCommand(projectAddCmd)

	projectAddCmd.Flags().StringP("name", "n", "", "Override project name (defaults to repo name)")
	projectAddCmd.Flags().StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	projectAddCmd.Flags().StringP("local", "l", "", "Add existing local repository instead of cloning")
	projectAddCmd.Flags().Bool("no-commit", false, "Skip automatic git commit")
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	source := args[0]

	name, _ := cmd.Flags().GetString("name")
	path, _ := cmd.Flags().GetString("path")
	local, _ := cmd.Flags().GetString("local")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	opts := project.AddOptions{
		Name:  name,
		Path:  path,
		Local: local,
	}

	// If --local flag is set, use its value as source
	if local != "" {
		source = local
	}

	result, err := project.Add(ctx, root, source, opts)
	if err != nil {
		// Check if it's a GitError and format it nicely
		var gitErr *project.GitError
		if errors.As(err, &gitErr) {
			return formatGitError(gitErr)
		}
		return err
	}

	// Print result
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Added project: "+result.Name))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Path:", result.Path))
	fmt.Println(ui.KeyValue("  Source:", result.Source))
	if result.Type != "" {
		fmt.Println(ui.KeyValue("  Type:", result.Type))
	}

	// Auto-commit if not disabled
	if !noCommit {
		cfg, _ := config.LoadCampaignConfig(ctx, root)
		campaignID := ""
		if cfg != nil {
			campaignID = cfg.ID
		}
		files := commit.NormalizeFiles(root, ".gitmodules", result.Path)
		commitResult := commit.Project(ctx, commit.ProjectOptions{
			Options: commit.Options{
				CampaignRoot: root,
				CampaignID:   campaignID,
				Files:        files,
			},
			Action:      commit.ProjectAdd,
			ProjectName: result.Name,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}

// formatGitError formats a GitError with nice visual indicators.
func formatGitError(gitErr *project.GitError) error {
	var b strings.Builder

	// Header with X indicator
	b.WriteString(ui.ErrorIcon())
	b.WriteString(" ")
	b.WriteString(ui.Error(gitErr.Diagnosis))
	b.WriteString("\n")

	// Fix instructions if present
	if gitErr.Fix != "" {
		b.WriteString("\n")
		b.WriteString(ui.Info(gitErr.Fix))
		b.WriteString("\n")
	}

	// Documentation link if present
	if gitErr.DocLink != "" {
		b.WriteString("\n")
		b.WriteString(ui.Label("Documentation:"))
		b.WriteString(" ")
		b.WriteString(ui.Accent(gitErr.DocLink))
		b.WriteString("\n")
	}

	return fmt.Errorf("%s", b.String())
}
