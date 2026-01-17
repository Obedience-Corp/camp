package main

import (
	"fmt"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/project"
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
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	source := args[0]

	name, _ := cmd.Flags().GetString("name")
	path, _ := cmd.Flags().GetString("path")
	local, _ := cmd.Flags().GetString("local")

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
		return err
	}

	// Print result
	fmt.Printf("Added project: %s\n", result.Name)
	fmt.Printf("  Path:   %s\n", result.Path)
	fmt.Printf("  Source: %s\n", result.Source)
	if result.Type != "" {
		fmt.Printf("  Type:   %s\n", result.Type)
	}

	return nil
}
