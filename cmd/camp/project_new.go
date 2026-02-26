package main

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var projectNewCmd = &cobra.Command{
	Use:   "new <name>",
	Short: "Create a new project in campaign",
	Long: `Create a new local project as a git submodule in the campaign.

The project is initialized as a git repository with an initial commit,
then added as a submodule under projects/. No remote repository is required.

You can add a remote later:
  cd projects/<name>
  git remote add origin git@github.com:org/<name>.git

Examples:
  camp project new my-service             # Create new project
  camp project new my-service --no-commit # Skip auto-commit to campaign`,
	Args: cobra.ExactArgs(1),
	RunE: runProjectNew,
}

func init() {
	projectCmd.AddCommand(projectNewCmd)

	projectNewCmd.Flags().StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	projectNewCmd.Flags().Bool("no-commit", false, "Skip automatic git commit")
}

func runProjectNew(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	name := args[0]

	path, _ := cmd.Flags().GetString("path")
	noCommit, _ := cmd.Flags().GetBool("no-commit")

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	opts := project.NewOptions{
		Path: path,
	}

	result, err := project.New(ctx, root, name, opts)
	if err != nil {
		var gitErr *project.GitError
		if errors.As(err, &gitErr) {
			return formatGitError(gitErr)
		}
		return err
	}

	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Created project: "+result.Name))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Path:", result.Path))
	fmt.Println(ui.KeyValue("  Source:", result.Source))

	if !noCommit {
		cfg, _ := config.LoadCampaignConfig(ctx, root)
		campaignID := ""
		if cfg != nil {
			campaignID = cfg.ID
		}
		commitResult := commit.Project(ctx, commit.ProjectOptions{
			Options: commit.Options{
				CampaignRoot: root,
				CampaignID:   campaignID,
			},
			Action:      commit.ProjectNew,
			ProjectName: result.Name,
		})
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}

	return nil
}
