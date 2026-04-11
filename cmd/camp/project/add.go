package project

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectAddCmd = &cobra.Command{
	Use:   "add [source]",
	Short: "Add a project to campaign",
	Long: `Add a git repository as a project in the campaign.

The project is cloned as a git submodule into the projects/ directory.
A worktree directory is also created for future parallel development.

Source can be:
  - SSH URL:   git@github.com:org/repo.git
  - HTTPS URL: https://github.com/org/repo.git
  - Local path (with --local): ./existing-repo
  - Local path (with --link):  ~/code/my-project

Examples:
  camp project add git@github.com:org/api.git           # Add remote repo
  camp project add https://github.com/org/web.git       # Add via HTTPS
  camp project add --local ./my-repo --name my-project  # Add existing local repo
  camp project add --link ~/code/my-project             # Link an existing project
  camp project add git@github.com:org/api.git --name backend  # Custom name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runProjectAdd,
}

func init() {
	Cmd.AddCommand(projectAddCmd)

	projectAddCmd.Flags().StringP("name", "n", "", "Override project name (defaults to repo name)")
	projectAddCmd.Flags().StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	projectAddCmd.Flags().StringP("local", "l", "", "Add existing local repository instead of cloning")
	projectAddCmd.Flags().String("link", "", "Link an existing local directory without cloning")
	projectAddCmd.Flags().Bool("no-commit", false, "Skip automatic git commit")
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	name, _ := cmd.Flags().GetString("name")
	path, _ := cmd.Flags().GetString("path")
	local, _ := cmd.Flags().GetString("local")
	link, _ := cmd.Flags().GetString("link")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	if local != "" && link != "" {
		return fmt.Errorf("use either --local or --link, not both")
	}

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	if link != "" {
		result, err := projectsvc.AddLinked(ctx, root, link, projectsvc.LinkOptions{
			Name: name,
			Path: path,
		})
		if err != nil {
			return err
		}

		fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Linked project: "+result.Name))
		fmt.Println()
		fmt.Println(ui.KeyValue("  Path:", result.Path))
		fmt.Println(ui.KeyValue("  Source:", result.Source))
		if result.Type != "" {
			fmt.Println(ui.KeyValue("  Type:", result.Type))
		}
		if result.IsGit {
			fmt.Println(ui.KeyValue("  Git:", "yes"))
		} else {
			fmt.Println(ui.KeyValue("  Git:", "no"))
		}
		fmt.Println()
		fmt.Println(ui.Dim("  Linked projects are machine-local and are not auto-committed."))
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("source URL is required")
	}
	source := args[0]

	opts := projectsvc.AddOptions{
		Name:  name,
		Path:  path,
		Local: local,
	}

	// If --local flag is set, use its value as source
	if local != "" {
		source = local
	}

	result, err := projectsvc.Add(ctx, root, source, opts)
	if err != nil {
		// Check if it's a GitError and format it nicely
		var gitErr *projectsvc.GitError
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
				CampaignRoot:  root,
				CampaignID:    campaignID,
				Files:         files,
				SelectiveOnly: true,
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
func formatGitError(gitErr *projectsvc.GitError) error {
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
