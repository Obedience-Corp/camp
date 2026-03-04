package main

import (
	"bufio"
	"context"
	"fmt"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/fest"
	"github.com/Obedience-Corp/camp/internal/git/commit"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/internal/ui/theme"
	"github.com/charmbracelet/huh"
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
	Annotations: map[string]string{
		"agent_allowed": "false",
		"agent_reason":  "Interactive campaign creation with huh forms",
		"interactive":   "true",
	},
	RunE: runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.GroupID = "setup"

	initCmd.Flags().StringP("name", "n", "", "Campaign name (defaults to directory name)")
	initCmd.Flags().StringP("type", "t", "product", "Campaign type (product, research, tools, personal)")
	initCmd.Flags().StringP("description", "d", "", "Campaign description")
	initCmd.Flags().StringP("mission", "m", "", "Campaign mission statement")
	initCmd.Flags().BoolP("force", "f", false, "Initialize in non-empty directory without prompting")
	initCmd.Flags().Bool("no-register", false, "Don't add to global registry")
	initCmd.Flags().Bool("no-git", false, "Skip git repository initialization")
	initCmd.Flags().Bool("dry-run", false, "Show what would be done without creating anything")
	initCmd.Flags().Bool("repair", false, "Add missing files to existing campaign")
	initCmd.Flags().Bool("yes", false, "Skip repair confirmation prompt (for scripting)")
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
	description, _ := cmd.Flags().GetString("description")
	mission, _ := cmd.Flags().GetString("mission")
	force, _ := cmd.Flags().GetBool("force")
	noRegister, _ := cmd.Flags().GetBool("no-register")
	noGit, _ := cmd.Flags().GetBool("no-git")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	repair, _ := cmd.Flags().GetBool("repair")
	yes, _ := cmd.Flags().GetBool("yes")
	skipFest, _ := cmd.Flags().GetBool("skip-fest")

	ctx := cmd.Context()
	isInteractive := tui.IsTerminal()

	// Early detection: error if already inside a campaign
	existingRoot, _ := campaign.Detect(ctx, dir)
	if existingRoot != "" {
		if repair {
			// Repair mode: use the detected campaign root regardless of where we are
			dir = existingRoot
		} else if !dryRun {
			cfg, _ := config.LoadCampaignConfig(ctx, existingRoot)
			name := filepath.Base(existingRoot)
			if cfg != nil && cfg.Name != "" {
				name = cfg.Name
			}
			return fmt.Errorf("already inside campaign '%s' at %s\n       Use 'camp init --repair' to add missing files", name, existingRoot)
		}
	}

	// Safety check for non-empty directory (skip for repair and dry-run)
	if !repair && !dryRun {
		if err := checkDirectoryEmpty(dir, force, isInteractive); err != nil {
			return err
		}
	}

	// Handle interactive mode for description and mission
	if !dryRun && !repair {
		var err error
		description, mission, err = collectCampaignInfo(ctx, description, mission, isInteractive)
		if err != nil {
			return err
		}
	}

	// Handle repair mode - check for missing mission
	if repair && !dryRun {
		var err error
		mission, err = handleRepairMission(ctx, dir, mission, isInteractive)
		if err != nil {
			return err
		}
	}

	opts := scaffold.InitOptions{
		Name:        name,
		Type:        config.CampaignType(typeStr),
		Description: description,
		Mission:     mission,
		NoRegister:  noRegister,
		SkipGitInit: noGit,
		DryRun:      dryRun,
		Repair:      repair,
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		return err
	}

	// Repair mode: compute and preview changes before applying.
	if repair && !dryRun {
		plan, err := scaffold.ComputeRepairPlan(ctx, dir, opts)
		if err != nil {
			return camperrors.Wrap(err, "failed to compute repair plan")
		}

		if !plan.HasChanges() {
			fmt.Println(ui.Success("Campaign is up to date — nothing to repair."))
			return nil
		}

		printRepairDiff(plan)

		if !yes {
			if !isInteractive {
				return fmt.Errorf("repair requires confirmation\n       Use --yes to skip the prompt in non-interactive mode")
			}
			fmt.Print("\nApply changes? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				fmt.Println("Repair cancelled.")
				return nil
			}
			fmt.Println()
		}

		opts.RepairPlan = plan
	}

	result, err := scaffold.Init(ctx, dir, opts)
	if err != nil {
		return err
	}

	// Execute migrations if repair detected misplaced directories.
	var migrationCount int
	if repair && opts.RepairPlan != nil && opts.RepairPlan.HasMigrations() {
		moved, err := scaffold.ExecuteMigrations(opts.RepairPlan.Migrations)
		if err != nil {
			fmt.Printf("  %s Migration error: %v\n", ui.WarningIcon(), err)
		}
		migrationCount = moved
	}

	// Auto-commit after repair (scaffold creates + migrations).
	if repair && !dryRun {
		commitRepairChanges(ctx, result, opts.RepairPlan, migrationCount)
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
	fmt.Println(ui.Dim("  go install github.com/Obedience-Corp/fest/cmd/fest@latest"))
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

// checkDirectoryEmpty verifies the target directory is empty or gets user approval.
func checkDirectoryEmpty(dir string, force, isInteractive bool) error {
	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve directory path")
	}

	// Check if directory exists
	info, err := os.Stat(absDir)
	if os.IsNotExist(err) {
		// Directory doesn't exist - will be created, so it's "empty"
		return nil
	}
	if err != nil {
		return camperrors.Wrap(err, "failed to check directory")
	}
	if !info.IsDir() {
		return fmt.Errorf("path exists but is not a directory: %s", absDir)
	}

	// Check if directory has contents
	entries, err := os.ReadDir(absDir)
	if err != nil {
		return camperrors.Wrap(err, "failed to read directory")
	}

	if len(entries) == 0 {
		// Directory is empty
		return nil
	}

	// Directory is not empty
	if force {
		// User explicitly approved via --force flag
		return nil
	}

	if isInteractive {
		// Prompt for confirmation
		fmt.Println(ui.Warning(fmt.Sprintf("Directory '%s' is not empty.", filepath.Base(absDir))))
		fmt.Print("Continue and initialize campaign here? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return camperrors.Wrap(err, "failed to read response")
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return fmt.Errorf("initialization cancelled")
		}
		fmt.Println()
		return nil
	}

	// Non-interactive mode without --force
	return fmt.Errorf("directory '%s' is not empty\n       Use --force to initialize anyway, or run in an interactive terminal to confirm", filepath.Base(absDir))
}

// collectCampaignInfo gathers description and mission from user input.
// In interactive mode, prompts via TUI. In non-interactive mode, requires flags.
func collectCampaignInfo(ctx context.Context, description, mission string, isInteractive bool) (string, string, error) {
	// If both are provided via flags, use them
	if description != "" && mission != "" {
		return description, mission, nil
	}

	// Non-interactive mode without required fields
	if !isInteractive {
		if description == "" && mission == "" {
			return "", "", fmt.Errorf("--description and --mission are required in non-interactive mode\n       Use -d/--description and -m/--mission flags, or run in an interactive terminal")
		}
		if description == "" {
			return "", "", fmt.Errorf("--description is required in non-interactive mode\n       Use -d/--description flag, or run in an interactive terminal")
		}
		if mission == "" {
			return "", "", fmt.Errorf("--mission is required in non-interactive mode\n       Use -m/--mission flag, or run in an interactive terminal")
		}
	}

	// Interactive mode - prompt for missing values
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Campaign Description").
				Description("A brief description of this campaign").
				Placeholder("e.g., AI agent orchestration framework").
				Value(&description),
			huh.NewText().
				Title("Mission Statement").
				Description("What is the goal of this campaign?").
				Placeholder("Describe the mission in detail...").
				CharLimit(1000).
				Value(&mission),
		),
	)

	if err := theme.RunForm(ctx, form); err != nil {
		if theme.IsCancelled(err) {
			return "", "", fmt.Errorf("initialization cancelled")
		}
		return "", "", camperrors.Wrap(err, "failed to collect campaign info")
	}

	// Validate that user provided values
	if description == "" {
		return "", "", fmt.Errorf("description is required")
	}
	if mission == "" {
		return "", "", fmt.Errorf("mission is required")
	}

	return description, mission, nil
}

// handleRepairMission checks for missing mission in existing campaign and prompts if needed.
func handleRepairMission(ctx context.Context, dir string, mission string, isInteractive bool) (string, error) {
	// If mission is provided via flag, use it
	if mission != "" {
		return mission, nil
	}

	// Load existing campaign config to check for mission
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil // Can't check, proceed without
	}

	cfg, err := config.LoadCampaignConfig(ctx, absDir)
	if err != nil {
		return "", nil // No existing config, proceed without
	}

	// If campaign already has a mission, use it
	if cfg.Mission != "" {
		return cfg.Mission, nil
	}

	// Campaign is missing mission
	if isInteractive {
		fmt.Println(ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement.", cfg.Name)))
		fmt.Println()

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewText().
					Title("Mission Statement").
					Description("What is the goal of this campaign?").
					Placeholder("Describe the mission in detail...").
					CharLimit(1000).
					Value(&mission),
			),
		)

		if err := theme.RunForm(ctx, form); err != nil {
			if theme.IsCancelled(err) {
				// User cancelled, proceed without mission
				fmt.Println(ui.Dim("Skipping mission statement"))
				return "", nil
			}
			return "", camperrors.Wrap(err, "failed to collect mission")
		}

		return mission, nil
	}

	// Non-interactive mode - just warn
	fmt.Println(ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement", cfg.Name)))
	fmt.Println(ui.Dim("         Run 'camp init --repair' in an interactive terminal to add one"))
	fmt.Println()
	return "", nil
}

// commitRepairChanges creates a git commit after a successful repair.
func commitRepairChanges(ctx context.Context, initResult *scaffold.InitResult, plan *scaffold.RepairPlan, migrationCount int) {
	hasChanges := len(initResult.DirsCreated) > 0 || len(initResult.FilesCreated) > 0 || migrationCount > 0
	if !hasChanges {
		return
	}

	cfg, err := config.LoadCampaignConfig(ctx, initResult.CampaignRoot)
	if err != nil {
		return
	}

	description := buildRepairCommitMessage(initResult, plan, migrationCount)
	files := buildRepairCommitFiles(initResult, plan)

	result := commit.Repair(ctx, commit.RepairOptions{
		Options: commit.Options{
			CampaignRoot:  initResult.CampaignRoot,
			CampaignID:    cfg.ID,
			Files:         files,
			SelectiveOnly: true,
		},
		Description: description,
	})

	if result.Committed {
		fmt.Printf("\n%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.Message != "" {
		fmt.Printf("\n%s %s\n", ui.InfoIcon(), result.Message)
	}
}

func buildRepairCommitFiles(initResult *scaffold.InitResult, plan *scaffold.RepairPlan) []string {
	files := make([]string, 0, len(initResult.FilesCreated)+len(initResult.DirsCreated))
	files = append(files, initResult.FilesCreated...)
	files = append(files, initResult.DirsCreated...)

	if plan != nil {
		for _, m := range plan.Migrations {
			for _, item := range m.Items {
				files = append(files,
					filepath.Join(m.Source, item),
					filepath.Join(m.Dest, item),
				)
			}
		}
	}

	return commit.NormalizeFiles(initResult.CampaignRoot, files...)
}

// buildRepairCommitMessage constructs a descriptive commit body for repair operations.
func buildRepairCommitMessage(initResult *scaffold.InitResult, plan *scaffold.RepairPlan, migrationCount int) string {
	var b strings.Builder

	if len(initResult.DirsCreated) > 0 {
		fmt.Fprintf(&b, "Directories created:\n")
		for _, d := range initResult.DirsCreated {
			fmt.Fprintf(&b, "  - %s\n", d)
		}
		b.WriteString("\n")
	}

	if len(initResult.FilesCreated) > 0 {
		fmt.Fprintf(&b, "Files created:\n")
		for _, f := range initResult.FilesCreated {
			fmt.Fprintf(&b, "  - %s\n", f)
		}
		b.WriteString("\n")
	}

	if plan != nil && migrationCount > 0 {
		fmt.Fprintf(&b, "Migrated %d item(s):\n", migrationCount)
		for _, m := range plan.Migrations {
			for _, item := range m.Items {
				fmt.Fprintf(&b, "  - %s → %s\n", filepath.Join(m.Source, item), m.Dest)
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

// printRepairDiff displays the proposed repair changes as a colored diff.
func printRepairDiff(plan *scaffold.RepairPlan) {
	fmt.Println(ui.Subheader("Repair Preview"))
	fmt.Println()

	for _, c := range plan.Changes {
		switch c.Type {
		case scaffold.RepairAdd:
			fmt.Printf("  %s  %s  %s\n",
				ui.Success("+"),
				ui.Success(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairModify:
			fmt.Printf("  %s  %s  %s\n",
				ui.Warning("~"),
				ui.Warning(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairPreserve:
			fmt.Printf("  %s  %s  %s\n",
				ui.Dim("✓"),
				ui.Value(c.Key),
				ui.Dim("(user-defined, preserved)"),
			)
		case scaffold.RepairMigrate:
			fmt.Printf("  %s  %s  %s\n",
				ui.Warning("→"),
				ui.Value(c.Key),
				ui.Dim(c.Description),
			)
		}
	}
}
