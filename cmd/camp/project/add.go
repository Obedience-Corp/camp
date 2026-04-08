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
	Use:   "add <source>",
	Short: "Add a project to campaign",
	Long: `Add a git repository as a project in the campaign.

The project is cloned as a git submodule into the projects/ directory.
A worktree directory is also created for future parallel development.

Source can be:
  - SSH URL:   git@github.com:org/repo.git
  - HTTPS URL: https://github.com/org/repo.git
  - Local path (with --local): ./existing-repo
  - Local path (with --link):  ~/code/my-project

The --link flag creates a symlink to an external directory instead of
cloning. This is useful for projects already on your machine that you
want to include in the campaign without copying. Linked projects can
be git repos or plain directories. The symlink and metadata are
machine-local and won't be committed to the campaign repo.

Examples:
  camp project add git@github.com:org/api.git           # Add remote repo
  camp project add https://github.com/org/web.git       # Add via HTTPS
  camp project add --local ./my-repo --name my-project  # Add existing local repo as submodule
  camp project add --link ~/code/my-app                 # Link external project
  camp project add --link ~/code/my-app --name app      # Link with custom name
  camp project add git@github.com:org/api.git --name backend  # Custom name`,
	Args: cobra.MaximumNArgs(1),
	RunE: runProjectAdd,
}

func init() {
	Cmd.AddCommand(projectAddCmd)

	projectAddCmd.Flags().StringP("name", "n", "", "Override project name (defaults to repo name)")
	projectAddCmd.Flags().StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	projectAddCmd.Flags().StringP("local", "l", "", "Add existing local repository as submodule (clones into campaign)")
	projectAddCmd.Flags().String("link", "", "Link an external directory via symlink (no cloning, machine-local)")
	projectAddCmd.Flags().Bool("no-commit", false, "Skip automatic git commit")
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	name, _ := cmd.Flags().GetString("name")
	path, _ := cmd.Flags().GetString("path")
	local, _ := cmd.Flags().GetString("local")
	link, _ := cmd.Flags().GetString("link")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	// Handle --link: create symlink to external project
	if link != "" {
		linkOpts := projectsvc.LinkOptions{
			Name: name,
			Path: path,
		}
		linkResult, err := projectsvc.AddLinked(ctx, root, link, linkOpts)
		if err != nil {
			return err
		}

		fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Linked project: "+linkResult.Name))
		fmt.Println()
		fmt.Println(ui.KeyValue("  Path:", linkResult.Path))
		fmt.Println(ui.KeyValue("  Source:", linkResult.Source))
		if linkResult.Type != "" {
			fmt.Println(ui.KeyValue("  Type:", linkResult.Type))
		}
		if linkResult.IsGit {
			fmt.Println(ui.KeyValue("  Git:", "yes"))
		} else {
			fmt.Println(ui.KeyValue("  Git:", "no (non-git directory)"))
		}
		fmt.Println()
		fmt.Println(ui.Dim("  Linked projects are machine-local and not committed to the campaign repo."))
		return nil
	}

	// Require source arg for non-link add
	if len(args) == 0 {
		return fmt.Errorf("source URL is required\n" +
			"Hint: Provide a git URL, or use --link to symlink an existing directory")
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
