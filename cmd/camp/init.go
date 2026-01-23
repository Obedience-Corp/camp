package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/fest"
	"github.com/obediencecorp/camp/internal/scaffold"
	"github.com/obediencecorp/camp/internal/ui"
	"github.com/spf13/cobra"
)

var initCmd = &cobra.Command{
	Use:   "init [path]",
	Short: "Initialize a new campaign",
	Long: `Initialize a new campaign directory structure.

Creates the standard campaign directories:
  .campaign/              - Campaign configuration and metadata
  projects/               - Project repositories (submodules or worktrees)
  projects/worktrees/     - Git worktrees for parallel development
  festivals/              - Festival methodology workspace (via fest init)
  ai_docs/                - AI-generated documentation
  docs/                   - Human-authored documentation
  dungeon/                - Archived and deprioritized work
  workflow/               - Workflow management
  workflow/code_reviews/  - Code review notes and feedback
  workflow/pipelines/     - CI/CD pipeline definitions
  workflow/design/        - Design documents
  workflow/intents/       - Intent documents

Also creates:
  AGENTS.md     - AI agent instruction file
  CLAUDE.md     - Symlink to AGENTS.md

Initializes a git repository if not already inside one.

Use --no-git to skip git initialization.`,
	Example: `  camp init                      Initialize current directory
  camp init my-campaign          Create and initialize new directory
  camp init --name "My Project"  Set custom campaign name
  camp init --no-git             Skip git initialization
  camp init --dry-run            Preview without creating anything`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.GroupID = "setup"

	initCmd.Flags().StringP("name", "n", "", "Campaign name (defaults to directory name)")
	initCmd.Flags().StringP("type", "t", "product", "Campaign type (product, research, tools, personal)")
	initCmd.Flags().Bool("no-register", false, "Don't add to global registry")
	initCmd.Flags().Bool("no-git", false, "Skip git repository initialization")
	initCmd.Flags().Bool("dry-run", false, "Show what would be done without creating anything")
	initCmd.Flags().Bool("repair", false, "Add missing files to existing campaign")
	initCmd.Flags().Bool("skip-fest", false, "Skip automatic Festival Methodology initialization")
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
	noRegister, _ := cmd.Flags().GetBool("no-register")
	noGit, _ := cmd.Flags().GetBool("no-git")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	repair, _ := cmd.Flags().GetBool("repair")
	skipFest, _ := cmd.Flags().GetBool("skip-fest")

	opts := scaffold.InitOptions{
		Name:        name,
		Type:        config.CampaignType(typeStr),
		NoRegister:  noRegister,
		SkipGitInit: noGit,
		DryRun:      dryRun,
		Repair:      repair,
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

	// Initialize Festival Methodology (unless skipped or dry-run)
	var festInitialized bool
	if !dryRun && !skipFest {
		festInitialized, _ = initializeFestivals(ctx, result.CampaignRoot)
	} else if skipFest && !dryRun {
		fmt.Println(ui.Info("Skipping Festival Methodology (--skip-fest)"))
	}

	// Print results
	if dryRun {
		fmt.Println(ui.Warning("Dry run - would create:"))
	} else if repair {
		fmt.Println(ui.Success("✓ Campaign Repaired"))
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
		if festInitialized {
			fmt.Println(ui.KeyValueColored("Festivals:", "initialized", ui.SuccessColor))
		}
	}

	return nil
}

// initializeFestivals runs fest init in the campaign directory.
// Returns true if successful, false with guidance if fest is unavailable.
func initializeFestivals(ctx context.Context, campaignRoot string) (bool, error) {
	// Check if already initialized
	if fest.IsInitialized(campaignRoot) {
		fmt.Println(ui.Success("Festival Methodology already initialized"))
		return true, nil
	}

	// Check if fest CLI is available
	if !fest.IsFestAvailable() {
		showFestInstallGuidance()
		return false, fest.ErrFestNotFound
	}

	// Check if festivals directory has content but isn't initialized
	if hasNonFestContent(campaignRoot) {
		showFestManualInitGuidance()
		return false, nil
	}

	fmt.Println(ui.Info("Initializing Festival Methodology..."))
	err := fest.RunInit(ctx, &fest.InitOptions{
		CampaignRoot: campaignRoot,
	})
	if err != nil {
		showFestInitFailure(err)
		return false, err
	}

	fmt.Println(ui.Success("Festival Methodology initialized"))
	return true, nil
}

// hasNonFestContent checks if festivals/ has content that isn't fest-initialized.
func hasNonFestContent(campaignRoot string) bool {
	festivalsDir := filepath.Join(campaignRoot, "festivals")
	entries, err := os.ReadDir(festivalsDir)
	if err != nil {
		return false
	}
	// If we have entries but fest isn't initialized, there's non-fest content
	return len(entries) > 0 && !fest.IsInitialized(campaignRoot)
}

// showFestInstallGuidance displays guidance for installing fest CLI.
func showFestInstallGuidance() {
	fmt.Println()
	fmt.Println(ui.Dim("Festival Methodology provides structured project planning."))
	fmt.Println(ui.Dim("Install the fest CLI to enable it:"))
	fmt.Println()
	fmt.Println(ui.Dim("  go install github.com/obediencecorp/fest/cmd/fest@latest"))
	fmt.Println()
	fmt.Println(ui.Dim("Then run: camp init --repair"))
	fmt.Println(ui.Dim("Continuing without Festival Methodology..."))
}

// showFestManualInitGuidance displays guidance when festivals/ has non-fest content.
func showFestManualInitGuidance() {
	fmt.Println()
	fmt.Println(ui.Warning("festivals/ has content but is not fest-initialized"))
	fmt.Println(ui.Dim("Run 'fest init' manually to initialize, or clear the directory."))
}

// showFestInitFailure displays guidance when fest init fails.
func showFestInitFailure(err error) {
	fmt.Println(ui.Warning(fmt.Sprintf("Failed to initialize Festival Methodology: %v", err)))
	fmt.Println()
	fmt.Println(ui.Dim("You may need to run 'fest init' manually."))
	fmt.Println(ui.Dim("Use 'fest init --help' for options."))
	fmt.Println(ui.Dim("Continuing with campaign creation..."))
}
