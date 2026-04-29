package initcmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/nav/tui"
	"github.com/Obedience-Corp/camp/internal/scaffold"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// New builds the camp init command.
func New() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "init [path]",
		Short:   "Initialize a new campaign",
		GroupID: "setup",
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

	cmd.Flags().StringP("name", "n", "", "Campaign name (defaults to directory name)")
	cmd.Flags().StringP("type", "t", "product", "Campaign type (product, research, tools, personal)")
	cmd.Flags().StringP("description", "d", "", "Campaign description")
	cmd.Flags().StringP("mission", "m", "", "Campaign mission statement")
	cmd.Flags().BoolP("force", "f", false, "Initialize in non-empty directory without prompting")
	cmd.Flags().Bool("no-register", false, "Don't add to global registry")
	cmd.Flags().Bool("no-git", false, "Skip git repository initialization")
	cmd.Flags().Bool("dry-run", false, "Show what would be done without creating anything")
	cmd.Flags().Bool("repair", false, "Add missing files to existing campaign")
	cmd.Flags().Bool("yes", false, "Skip repair confirmation prompt (for scripting)")
	cmd.Flags().Bool("skip-fest", false, "Skip automatic Festival Methodology initialization")

	return cmd
}

// Params is the full set of inputs the init flow needs, already
// parsed from flags or constructed by another command (e.g., create).
type Params struct {
	Dir           string
	Name          string
	TypeStr       string
	Description   string
	Mission       string
	Force         bool
	NoRegister    bool
	NoGit         bool
	DryRun        bool
	Repair        bool
	Yes           bool
	SkipFest      bool
	VerboseOutput bool
}

// Writers routes init flow output for command callers.
type Writers struct {
	HumanOut io.Writer
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

// ChooseWriters returns the default writer set for command output.
func ChooseWriters() Writers {
	return Writers{HumanOut: os.Stdout}
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
	verboseOutput := cmdutil.GetFlagBool(cmd, "verbose")
	p := Params{
		Dir:           absDir,
		Name:          cmdutil.GetFlagString(cmd, "name"),
		TypeStr:       cmdutil.GetFlagString(cmd, "type"),
		Description:   cmdutil.GetFlagString(cmd, "description"),
		Mission:       cmdutil.GetFlagString(cmd, "mission"),
		Force:         cmdutil.GetFlagBool(cmd, "force"),
		NoRegister:    cmdutil.GetFlagBool(cmd, "no-register"),
		NoGit:         cmdutil.GetFlagBool(cmd, "no-git"),
		DryRun:        cmdutil.GetFlagBool(cmd, "dry-run"),
		Repair:        cmdutil.GetFlagBool(cmd, "repair"),
		Yes:           cmdutil.GetFlagBool(cmd, "yes"),
		SkipFest:      cmdutil.GetFlagBool(cmd, "skip-fest"),
		VerboseOutput: verboseOutput,
	}
	w := ChooseWriters()
	return RunFlow(cmd.Context(), p, w, tui.IsTerminal())
}

func RunFlow(ctx context.Context, p Params, w Writers, isInteractive bool) error {
	dir := p.Dir

	// Early detection: error if already inside a campaign
	existingRoot, _ := campaign.Detect(ctx, dir)
	if existingRoot != "" {
		if p.Repair {
			// Repair mode: use the detected campaign root regardless of where we are
			dir = existingRoot
		} else if !p.DryRun {
			cfg, _ := config.LoadCampaignConfig(ctx, existingRoot)
			name := filepath.Base(existingRoot)
			if cfg != nil && cfg.Name != "" {
				name = cfg.Name
			}
			return fmt.Errorf("already inside campaign '%s' at %s\n       Use 'camp init --repair' to add missing files", name, existingRoot)
		}
	}

	// Safety check for non-empty directory (skip for repair and dry-run)
	if !p.Repair && !p.DryRun {
		if err := checkDirectoryEmpty(dir, p.Force, isInteractive, w); err != nil {
			return err
		}
	}

	description := p.Description
	mission := p.Mission

	// Handle interactive mode for description and mission
	if !p.DryRun && !p.Repair {
		var err error
		description, mission, err = collectCampaignInfo(ctx, description, mission, isInteractive)
		if err != nil {
			return err
		}
	}

	// Handle repair mode - check for missing mission
	if p.Repair && !p.DryRun {
		var err error
		mission, err = handleRepairMission(ctx, dir, mission, isInteractive, w)
		if err != nil {
			return err
		}
	}

	opts := scaffold.InitOptions{
		Name:        p.Name,
		Type:        config.CampaignType(p.TypeStr),
		Description: description,
		Mission:     mission,
		NoRegister:  p.NoRegister,
		SkipGitInit: p.NoGit,
		DryRun:      p.DryRun,
		Repair:      p.Repair,
	}

	// Validate options
	if err := opts.Validate(); err != nil {
		return err
	}

	// Repair mode: compute and preview changes before applying.
	if p.Repair && !p.DryRun {
		plan, err := scaffold.ComputeRepairPlan(ctx, dir, opts)
		if err != nil {
			return camperrors.Wrap(err, "failed to compute repair plan")
		}

		if !plan.HasChanges() {
			writeLine(w.HumanOut, ui.Success("Campaign is up to date — nothing to repair."))
			return nil
		}

		printRepairDiff(plan, w)

		if !p.Yes {
			if !isInteractive {
				return fmt.Errorf("repair requires confirmation\n       Use --yes to skip the prompt in non-interactive mode")
			}
			write(w.HumanOut, "\nApply changes? [y/N] ")
			reader := bufio.NewReader(os.Stdin)
			answer, _ := reader.ReadString('\n')
			answer = strings.TrimSpace(strings.ToLower(answer))
			if answer != "y" && answer != "yes" {
				writeLine(w.HumanOut, "Repair cancelled.")
				return nil
			}
			writeLine(w.HumanOut)
		}

		opts.RepairPlan = plan
	}

	result, err := scaffold.Init(ctx, dir, opts)
	if err != nil {
		return err
	}

	// Execute migrations if repair detected misplaced directories.
	var migrationCount int
	if p.Repair && opts.RepairPlan != nil && len(opts.RepairPlan.Migrations) > 0 {
		moved, err := scaffold.ExecuteMigrations(opts.RepairPlan.Migrations)
		if err != nil {
			writef(w.HumanOut, "  %s Migration error: %v\n", ui.WarningIcon(), err)
		}
		migrationCount = moved
	}

	// Auto-commit after repair (scaffold creates + migrations).
	if p.Repair && !p.DryRun {
		commitRepairChanges(ctx, result, opts.RepairPlan, migrationCount, w)
	}

	// Initialize Festival Methodology (unless skipped or dry-run).
	var festInitialized bool
	if !p.DryRun && !p.SkipFest {
		festInitialized, _ = initializeFestivals(ctx, result.CampaignRoot, w)
	} else if p.SkipFest && !p.DryRun {
		writeLine(w.HumanOut, ui.Info("Skipping Festival Methodology (--skip-fest)"))
	}

	// Print results
	if p.DryRun {
		writeLine(w.HumanOut, ui.Warning("Dry run - would create:"))
	} else if p.Repair {
		writeLine(w.HumanOut, ui.Success("✓ Campaign Repaired"))
	} else {
		writeLine(w.HumanOut, ui.Success("✓ Campaign Initialized"))
	}

	if len(result.DirsCreated) > 0 {
		writeLine(w.HumanOut)
		writeLine(w.HumanOut, ui.Subheader("Directories created:"))
		for _, d := range result.DirsCreated {
			writef(w.HumanOut, "  %s %s\n", ui.SuccessIcon(), ui.Value(d))
		}
	}

	if len(result.FilesCreated) > 0 {
		writeLine(w.HumanOut)
		writeLine(w.HumanOut, ui.Subheader("Files created:"))
		for _, f := range result.FilesCreated {
			writef(w.HumanOut, "  %s %s\n", ui.SuccessIcon(), ui.Value(f))
		}
	}

	if len(result.Skipped) > 0 && p.VerboseOutput {
		writeLine(w.HumanOut)
		writeLine(w.HumanOut, ui.Subheader("Skipped (already exist):"))
		for _, s := range result.Skipped {
			writef(w.HumanOut, "  %s %s\n", ui.WarningIcon(), ui.Dim(s))
		}
	}

	if !p.DryRun {
		writeLine(w.HumanOut)
		typeColor := ui.GetCampaignTypeColor(string(opts.Type))
		writeLine(w.HumanOut, ui.KeyValue("Campaign:", result.Name))
		writeLine(w.HumanOut, ui.KeyValueColored("Type:", string(opts.Type), typeColor))
		writeLine(w.HumanOut, ui.KeyValue("ID:", result.ID))
		writeLine(w.HumanOut, ui.KeyValue("Root:", result.CampaignRoot))
		if result.GitInitialized {
			writeLine(w.HumanOut, ui.KeyValueColored("Git:", "initialized", ui.SuccessColor))
		}
		if festInitialized {
			writeLine(w.HumanOut, ui.KeyValueColored("Festivals:", "initialized", ui.SuccessColor))
		}
	}

	return nil
}
