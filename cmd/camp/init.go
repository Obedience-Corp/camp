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
	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
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
