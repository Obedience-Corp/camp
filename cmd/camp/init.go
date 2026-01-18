package main

import (
	"fmt"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/scaffold"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new campaign",
	Long: `Initialize a new campaign directory structure.

Creates the standard campaign directories:
  .campaign/    - Campaign configuration and metadata
  projects/     - Project repositories (submodules or worktrees)
  festivals/    - Festival methodology workspace
  worktrees/    - Git worktrees for parallel development
  ai_docs/      - AI-generated documentation
  docs/         - Human-authored documentation
  corpus/       - Reference materials and knowledge base
  pipelines/    - CI/CD pipeline definitions
  code_reviews/ - Code review notes and feedback

Also creates:
  CLAUDE.md     - AI agent instruction file
  AGENTS.md     - Symlink to CLAUDE.md

Initializes a git repository if not already inside one.

Use --minimal for just .campaign/ and projects/.
Use --no-git to skip git initialization.`,
	Example: `  camp init                      Initialize current directory
  camp init my-campaign          Create and initialize new directory
  camp init --name "My Project"  Set custom campaign name
  camp init --minimal            Minimal structure (.campaign/, projects/)
  camp init --no-git             Skip git initialization
  camp init --dry-run            Preview without creating anything`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)

	initCmd.Flags().StringP("name", "n", "", "Campaign name (defaults to directory name)")
	initCmd.Flags().StringP("type", "t", "product", "Campaign type (product, research, tools, personal)")
	initCmd.Flags().BoolP("minimal", "m", false, "Create minimal directory structure")
	initCmd.Flags().Bool("no-register", false, "Don't add to global registry")
	initCmd.Flags().Bool("no-git", false, "Skip git repository initialization")
	initCmd.Flags().Bool("dry-run", false, "Show what would be done without creating anything")
}

func runInit(cmd *cobra.Command, args []string) error {
	// Determine target directory
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}

	// Parse flags
	name, _ := cmd.Flags().GetString("name")
	typeStr, _ := cmd.Flags().GetString("type")
	minimal, _ := cmd.Flags().GetBool("minimal")
	noRegister, _ := cmd.Flags().GetBool("no-register")
	noGit, _ := cmd.Flags().GetBool("no-git")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	opts := scaffold.InitOptions{
		Name:        name,
		Type:        config.CampaignType(typeStr),
		Minimal:     minimal,
		NoRegister:  noRegister,
		SkipGitInit: noGit,
		DryRun:      dryRun,
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		return err
	}

	ctx := cmd.Context()
	result, err := scaffold.Init(ctx, dir, opts)
	if err != nil {
		return err
	}

	// Print results
	if dryRun {
		fmt.Println(ui.Warning("Dry run - would create:"))
	} else {
		fmt.Println(ui.Success("✓ Campaign Initialized"))
	}

	if len(result.DirsCreated) > 0 {
		fmt.Println()
		fmt.Println(ui.Subheader("Directories created:"))
		for _, d := range result.DirsCreated {
			fmt.Printf("  %s %s\n", ui.SuccessIcon(), ui.Value(d))
		}
	}

	if len(result.FilesCreated) > 0 {
		fmt.Println()
		fmt.Println(ui.Subheader("Files created:"))
		for _, f := range result.FilesCreated {
			fmt.Printf("  %s %s\n", ui.SuccessIcon(), ui.Value(f))
		}
	}

	if len(result.Skipped) > 0 && verbose {
		fmt.Println()
		fmt.Println(ui.Subheader("Skipped (already exist):"))
		for _, s := range result.Skipped {
			fmt.Printf("  %s %s\n", ui.WarningIcon(), ui.Dim(s))
		}
	}

	if !dryRun {
		fmt.Println()
		typeColor := ui.GetCampaignTypeColor(string(opts.Type))
		fmt.Println(ui.KeyValue("Campaign:", result.Name))
		fmt.Println(ui.KeyValueColored("Type:", string(opts.Type), typeColor))
		fmt.Println(ui.KeyValue("ID:", result.ID))
		fmt.Println(ui.KeyValue("Root:", result.CampaignRoot))
		if result.GitInitialized {
			fmt.Println(ui.KeyValueColored("Git:", "initialized", ui.SuccessColor))
		}
	}

	return nil
}
