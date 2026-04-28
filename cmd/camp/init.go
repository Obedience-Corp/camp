package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
  .campaign/quests/       - Quest execution contexts
  .campaign/intents/      - System-managed intent state
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

Also creates:
  AGENTS.md     - AI agent instruction file
  CLAUDE.md     - Symlink to AGENTS.md

Initializes a git repository if not already inside one.

Use --no-git to skip git initialization.`,
	Example: `  camp init                                        Initialize current directory
  camp init my-campaign                            Create and initialize new directory
  camp init --name "My Project"                    Set custom campaign name
  camp init --no-git                               Skip git initialization
  camp init --dry-run                              Preview without creating anything
  camp init --print-path -d "desc" -m "mission"   Machine mode: root on stdout, summary on stderr`,
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
	initCmd.Flags().Bool("print-path", false, "Print the new campaign root path to stdout (machine mode)")
}

// initParams is the full set of inputs the init flow needs, already
// parsed from flags or constructed by another command (e.g., create).
type initParams struct {
	dir           string
	name          string
	typeStr       string
	description   string
	mission       string
	force         bool
	noRegister    bool
	noGit         bool
	dryRun        bool
	repair        bool
	yes           bool
	skipFest      bool
	verboseOutput bool
	printPath     bool
}

// initWriters routes output between human-facing text and machine-
// readable lines. In default mode humanOut == machineOut == os.Stdout.
// In print-path mode humanOut == os.Stderr and machineOut == os.Stdout.
type initWriters struct {
	humanOut   io.Writer
	machineOut io.Writer
}

func write(w io.Writer, args ...any) {
	_, _ = fmt.Fprint(w, args...)
}

func writeLine(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

func writef(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// chooseInitWriters returns the correct writer pair for the given mode.
// In default mode both writers point to stdout so behavior is unchanged.
// In print-path mode human-readable output goes to stderr (the conventional
// channel for interactive/status messages) and machine output goes to stdout.
func chooseInitWriters(printPath bool) initWriters {
	if printPath {
		return initWriters{humanOut: os.Stderr, machineOut: os.Stdout}
	}
	return initWriters{humanOut: os.Stdout, machineOut: os.Stdout}
}

func runInit(cmd *cobra.Command, args []string) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return camperrors.Wrap(err, "failed to resolve directory path")
	}
	verboseOutput := getFlagBool(cmd, "verbose")
	printPath := getFlagBool(cmd, "print-path")
	p := initParams{
		dir:           absDir,
		name:          getFlagString(cmd, "name"),
		typeStr:       getFlagString(cmd, "type"),
		description:   getFlagString(cmd, "description"),
		mission:       getFlagString(cmd, "mission"),
		force:         getFlagBool(cmd, "force"),
		noRegister:    getFlagBool(cmd, "no-register"),
		noGit:         getFlagBool(cmd, "no-git"),
		dryRun:        getFlagBool(cmd, "dry-run"),
		repair:        getFlagBool(cmd, "repair"),
		yes:           getFlagBool(cmd, "yes"),
		skipFest:      getFlagBool(cmd, "skip-fest"),
		verboseOutput: verboseOutput,
		printPath:     printPath,
	}
	w := chooseInitWriters(p.printPath)
	return runInitFlow(cmd.Context(), p, w, tui.IsTerminal())
}

func runInitFlow(ctx context.Context, p initParams, w initWriters, isInteractive bool) error {
	dir := p.dir

	// Early detection: error if already inside a campaign
	existingRoot, _ := campaign.Detect(ctx, dir)
	if existingRoot != "" {
		if p.repair {
			// Repair mode: use the detected campaign root regardless of where we are
			dir = existingRoot
		} else if !p.dryRun {
			cfg, _ := config.LoadCampaignConfig(ctx, existingRoot)
			name := filepath.Base(existingRoot)
			if cfg != nil && cfg.Name != "" {
				name = cfg.Name
			}
			return fmt.Errorf("already inside campaign '%s' at %s\n       Use 'camp init --repair' to add missing files", name, existingRoot)
		}
	}

	// Safety check for non-empty directory (skip for repair and dry-run)
	if !p.repair && !p.dryRun {
		if err := checkDirectoryEmpty(dir, p.force, isInteractive, w); err != nil {
			return err
		}
	}

	description := p.description
	mission := p.mission

	// Handle interactive mode for description and mission
	if !p.dryRun && !p.repair {
		var err error
		description, mission, err = collectCampaignInfo(ctx, description, mission, isInteractive)
		if err != nil {
			return err
		}
	}

	// Handle repair mode - check for missing mission
	if p.repair && !p.dryRun {
		var err error
		mission, err = handleRepairMission(ctx, dir, mission, isInteractive, w)
		if err != nil {
			return err
		}
	}

	opts := scaffold.InitOptions{
		Name:        p.name,
		Type:        config.CampaignType(p.typeStr),
		Description: description,
		Mission:     mission,
		NoRegister:  p.noRegister,
		SkipGitInit: p.noGit,
		SkipFest:    p.skipFest,
		DryRun:      p.dryRun,
		Repair:      p.repair,
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		return err
	}

	// Repair mode: compute and preview changes before applying.
	if p.repair && !p.dryRun {
		plan, err := scaffold.ComputeRepairPlan(ctx, dir, opts)
		if err != nil {
			return camperrors.Wrap(err, "failed to compute repair plan")
		}

		if !plan.HasChanges() {
			writeLine(w.humanOut, ui.Success("Campaign is up to date — nothing to repair."))
			return nil
		}

		printRepairDiff(plan, w)

		if !p.yes {
			if !isInteractive {
				return fmt.Errorf("repair requires confirmation\n       Use --yes to skip the prompt in non-interactive mode")
			}
			write(w.humanOut, "\nApply changes? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				writeLine(w.humanOut, "Repair cancelled.")
				return nil
			}
			writeLine(w.humanOut)
		}

		opts.RepairPlan = plan
	}

	result, err := scaffold.Init(ctx, dir, opts)
	if err != nil {
		return err
	}

	// Execute migrations if repair detected misplaced directories.
	var migrationCount int
	if p.repair && opts.RepairPlan != nil && len(opts.RepairPlan.Migrations) > 0 {
		moved, err := scaffold.ExecuteMigrations(opts.RepairPlan.Migrations)
		if err != nil {
			writef(w.humanOut, "  %s Migration error: %v\n", ui.WarningIcon(), err)
		}
		migrationCount = moved
	}

	// Auto-commit after repair (scaffold creates + migrations).
	if p.repair && !p.dryRun {
		commitRepairChanges(ctx, result, opts.RepairPlan, migrationCount, w)
	}

	// Initialize Festival Methodology (unless skipped or dry-run)
	var festInitialized bool
	if !p.dryRun && !p.skipFest {
		festInitialized, _ = initializeFestivals(ctx, result.CampaignRoot, w)
	} else if p.skipFest && !p.dryRun {
		writeLine(w.humanOut, ui.Info("Skipping Festival Methodology (--skip-fest)"))
	}

	// Print results
	if p.dryRun {
		writeLine(w.humanOut, ui.Warning("Dry run - would create:"))
	} else if p.repair {
		writeLine(w.humanOut, ui.Success("✓ Campaign Repaired"))
	} else {
		writeLine(w.humanOut, ui.Success("✓ Campaign Initialized"))
	}

	if len(result.DirsCreated) > 0 {
		writeLine(w.humanOut)
		writeLine(w.humanOut, ui.Subheader("Directories created:"))
		for _, d := range result.DirsCreated {
			writef(w.humanOut, "  %s %s\n", ui.SuccessIcon(), ui.Value(d))
		}
	}

	if len(result.FilesCreated) > 0 {
		writeLine(w.humanOut)
		writeLine(w.humanOut, ui.Subheader("Files created:"))
		for _, f := range result.FilesCreated {
			writef(w.humanOut, "  %s %s\n", ui.SuccessIcon(), ui.Value(f))
		}
	}

	if len(result.Skipped) > 0 && p.verboseOutput {
		writeLine(w.humanOut)
		writeLine(w.humanOut, ui.Subheader("Skipped (already exist):"))
		for _, s := range result.Skipped {
			writef(w.humanOut, "  %s %s\n", ui.WarningIcon(), ui.Dim(s))
		}
	}

	if !p.dryRun {
		writeLine(w.humanOut)
		typeColor := ui.GetCampaignTypeColor(string(opts.Type))
		writeLine(w.humanOut, ui.KeyValue("Campaign:", result.Name))
		writeLine(w.humanOut, ui.KeyValueColored("Type:", string(opts.Type), typeColor))
		writeLine(w.humanOut, ui.KeyValue("ID:", result.ID))
		writeLine(w.humanOut, ui.KeyValue("Root:", result.CampaignRoot))
		if result.GitInitialized {
			writeLine(w.humanOut, ui.KeyValueColored("Git:", "initialized", ui.SuccessColor))
		}
		if festInitialized {
			writeLine(w.humanOut, ui.KeyValueColored("Festivals:", "initialized", ui.SuccessColor))
		}
	}

	// Machine output: emit the absolute campaign root to machineOut when
	// --print-path is set. Dry-run is excluded because no campaign root exists.
	if p.printPath && !p.dryRun {
		writeLine(w.machineOut, result.CampaignRoot)
	}

	return nil
}

// initializeFestivals runs fest init in the campaign directory.
// Returns true if successful, false with guidance if fest is unavailable.
func initializeFestivals(ctx context.Context, campaignRoot string, w initWriters) (bool, error) {
	// Check if already initialized
	if fest.IsInitialized(campaignRoot) {
		writeLine(w.humanOut, ui.Success("Festival Methodology already initialized"))
		return true, nil
	}

	// Check if fest CLI is available
	if !fest.IsFestAvailable() {
		showFestInstallGuidance(w)
		return false, fest.ErrFestNotFound
	}

	// Check if festivals directory has content but isn't initialized
	if hasNonFestContent(campaignRoot) {
		showFestManualInitGuidance(w)
		return false, nil
	}

	writeLine(w.humanOut, ui.Info("Initializing Festival Methodology..."))
	err := fest.RunInit(ctx, &fest.InitOptions{
		CampaignRoot: campaignRoot,
	})
	if err != nil {
		showFestInitFailure(err, w)
		return false, err
	}

	writeLine(w.humanOut, ui.Success("Festival Methodology initialized"))
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
func showFestInstallGuidance(w initWriters) {
	writeLine(w.humanOut)
	writeLine(w.humanOut, ui.Dim("Festival Methodology provides structured project planning."))
	writeLine(w.humanOut, ui.Dim("Install the fest CLI to enable it:"))
	writeLine(w.humanOut)
	writeLine(w.humanOut, ui.Dim("  go install github.com/Obedience-Corp/fest/cmd/fest@latest"))
	writeLine(w.humanOut)
	writeLine(w.humanOut, ui.Dim("Then run: camp init --repair"))
	writeLine(w.humanOut, ui.Dim("Continuing without Festival Methodology..."))
}

// showFestManualInitGuidance displays guidance when festivals/ has non-fest content.
func showFestManualInitGuidance(w initWriters) {
	writeLine(w.humanOut)
	writeLine(w.humanOut, ui.Warning("festivals/ has content but is not fest-initialized"))
	writeLine(w.humanOut, ui.Dim("Run 'fest init' manually to initialize, or clear the directory."))
}

// showFestInitFailure displays guidance when fest init fails.
func showFestInitFailure(err error, w initWriters) {
	writeLine(w.humanOut, ui.Warning(fmt.Sprintf("Failed to initialize Festival Methodology: %v", err)))
	writeLine(w.humanOut)
	writeLine(w.humanOut, ui.Dim("You may need to run 'fest init' manually."))
	writeLine(w.humanOut, ui.Dim("Use 'fest init --help' for options."))
	writeLine(w.humanOut, ui.Dim("Continuing with campaign creation..."))
}

// checkDirectoryEmpty verifies the target directory is empty or gets user approval.
func checkDirectoryEmpty(dir string, force, isInteractive bool, w initWriters) error {
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
		// Prompt for confirmation. The prompt text goes to humanOut (stderr in
		// print-path mode), which is the conventional channel for interactive prompts.
		writeLine(w.humanOut, ui.Warning(fmt.Sprintf("Directory '%s' is not empty.", filepath.Base(absDir))))
		write(w.humanOut, "Continue and initialize campaign here? [y/N]: ")

		reader := bufio.NewReader(os.Stdin)
		response, err := reader.ReadString('\n')
		if err != nil {
			return camperrors.Wrap(err, "failed to read response")
		}

		response = strings.TrimSpace(strings.ToLower(response))
		if response != "y" && response != "yes" {
			return fmt.Errorf("initialization cancelled")
		}
		writeLine(w.humanOut)
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
func handleRepairMission(ctx context.Context, dir string, mission string, isInteractive bool, w initWriters) (string, error) {
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
		writeLine(w.humanOut, ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement.", cfg.Name)))
		writeLine(w.humanOut)

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
				writeLine(w.humanOut, ui.Dim("Skipping mission statement"))
				return "", nil
			}
			return "", camperrors.Wrap(err, "failed to collect mission")
		}

		return mission, nil
	}

	// Non-interactive mode - just warn
	writeLine(w.humanOut, ui.Warning(fmt.Sprintf("Campaign '%s' is missing a mission statement", cfg.Name)))
	writeLine(w.humanOut, ui.Dim("         Run 'camp init --repair' in an interactive terminal to add one"))
	writeLine(w.humanOut)
	return "", nil
}

// commitRepairChanges creates a git commit after a successful repair.
func commitRepairChanges(ctx context.Context, initResult *scaffold.InitResult, plan *scaffold.RepairPlan, migrationCount int, w initWriters) {
	hasChanges := len(initResult.DirsCreated) > 0 || len(initResult.FilesCreated) > 0 || migrationCount > 0
	if plan != nil && len(plan.IntentMigrations) > 0 {
		hasChanges = true
	}
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
		writef(w.humanOut, "\n%s %s\n", ui.SuccessIcon(), result.Message)
	} else if result.Message != "" {
		writef(w.humanOut, "\n%s %s\n", ui.InfoIcon(), result.Message)
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
		for _, m := range plan.IntentMigrations {
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

	if plan != nil && len(plan.IntentMigrations) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		fmt.Fprintf(&b, "Migrated %d legacy intent item(s):\n", countMigrationItems(plan.IntentMigrations))
		for _, m := range plan.IntentMigrations {
			for _, item := range m.Items {
				fmt.Fprintf(&b, "  - %s → %s\n", filepath.Join(m.Source, item), m.Dest)
			}
		}
	}

	return strings.TrimRight(b.String(), "\n")
}

func countMigrationItems(migrations []scaffold.MigrationAction) int {
	total := 0
	for _, m := range migrations {
		total += len(m.Items)
	}
	return total
}

// printRepairDiff displays the proposed repair changes as a colored diff.
func printRepairDiff(plan *scaffold.RepairPlan, w initWriters) {
	writeLine(w.humanOut, ui.Subheader("Repair Preview"))
	writeLine(w.humanOut)

	for _, c := range plan.Changes {
		switch c.Type {
		case scaffold.RepairAdd:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Success("+"),
				ui.Success(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairModify:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Warning("~"),
				ui.Warning(c.Key),
				ui.Dim("("+c.Description+")"),
			)
		case scaffold.RepairPreserve:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Dim("✓"),
				ui.Value(c.Key),
				ui.Dim("(user-defined, preserved)"),
			)
		case scaffold.RepairMigrate:
			writef(w.humanOut, "  %s  %s  %s\n",
				ui.Warning("→"),
				ui.Value(c.Key),
				ui.Dim(c.Description),
			)
		}
	}
}
