package project

import (
	"errors"
	"fmt"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/prune"

	"github.com/Obedience-Corp/camp/internal/campaign"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

var projectPruneCmd = &cobra.Command{
	Use:   "prune [project-name]",
	Short: "Delete merged branches in a project",
	Long: `Delete local branches that have been merged into the default branch.

Auto-detects the current project from your working directory,
or accepts a project name as a positional argument.

Protected branches (default branch, current branch) are never deleted.

Examples:
  camp project prune                     # Prune current project
  camp project prune camp                # Prune by name
  camp project prune -p camp             # Prune by flag
  camp project prune --dry-run           # Preview what would be deleted
	camp project prune --remote            # Also prune stale remote tracking refs
  camp project prune --remote-delete     # Also delete merged branches on origin`,
	Args:              cobra.MaximumNArgs(1),
	RunE:              runProjectPrune,
	ValidArgsFunction: cmdutil.CompleteProjectName,
}

var (
	pruneProjectFlag  string
	pruneDryRun       bool
	pruneForce        bool
	pruneRemote       bool
	pruneRemoteDelete bool
)

func init() {
	projectPruneCmd.Flags().StringVarP(&pruneProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	projectPruneCmd.Flags().BoolVarP(&pruneDryRun, "dry-run", "n", false, "Preview without deleting")
	projectPruneCmd.Flags().BoolVarP(&pruneForce, "force", "f", false, "Skip local branch deletion confirmation")
	projectPruneCmd.Flags().BoolVar(&pruneRemote, "remote", false, "Also prune stale remote tracking refs")
	projectPruneCmd.Flags().BoolVar(&pruneRemoteDelete, "remote-delete", false, "Also delete merged branches on origin (destructive)")

	if err := projectPruneCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}

	Cmd.AddCommand(projectPruneCmd)
}

// pruneOptionsFromFlags constructs prune.Options from the package-level flag vars.
func pruneOptionsFromFlags() prune.Options {
	return prune.Options{
		DryRun:       pruneDryRun,
		Force:        pruneForce,
		Remote:       pruneRemote,
		RemoteDelete: pruneRemoteDelete,
	}
}

func runProjectPrune(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a campaign")
	}

	// Resolve project: positional arg > flag > cwd
	projectName := pruneProjectFlag
	if len(args) > 0 {
		projectName = args[0]
	}

	result, err := projectsvc.Resolve(ctx, campRoot, projectName)
	if err != nil {
		var notFound *projectsvc.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + projectsvc.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}

	pr := prune.Execute(ctx, result.Name, result.Path, pruneOptionsFromFlags())

	renderPruneResult(pr)

	return nil
}

// Package-level styles for prune output — allocated once.
var (
	pruneStyleGreen  = lipgloss.NewStyle().Foreground(ui.SuccessColor)
	pruneStyleRed    = lipgloss.NewStyle().Foreground(ui.ErrorColor)
	pruneStyleYellow = lipgloss.NewStyle().Foreground(ui.WarningColor)
	pruneStyleDim    = lipgloss.NewStyle().Foreground(ui.DimColor)
	pruneStyleHeader = lipgloss.NewStyle().Bold(true).Foreground(ui.BrightColor)
)

func renderPruneResult(pr prune.ProjectResult) {
	if pr.Error != "" {
		fmt.Printf("%s %s: %s\n", ui.ErrorIcon(), pr.Name, ui.Error(pr.Error))
		return
	}

	if len(pr.Results) == 0 && pr.Pruned == 0 {
		fmt.Printf("%s %s: %s\n", ui.SuccessIcon(), ui.Value(pr.Name), ui.Dim("no merged branches to prune"))
		return
	}

	fmt.Printf("\n%s %s\n", ui.Subheader("Project:"), ui.Value(pr.Name))

	if len(pr.Results) > 0 {
		fmt.Println(buildPruneTable(pr.Results))
	}

	if pr.Pruned > 0 {
		fmt.Printf("%s Pruned %d stale remote tracking ref(s)\n", ui.SuccessIcon(), pr.Pruned)
	}

	deleted := 0
	for _, r := range pr.Results {
		if r.Status == prune.StatusDeleted {
			deleted++
		}
	}
	if deleted > 0 {
		fmt.Printf("\n%s Pruned %d branch(es)\n", ui.SuccessIcon(), deleted)
	}
}

// buildPruneTable constructs the lipgloss table for prune results.
func buildPruneTable(results []prune.Result) *table.Table {
	headers := []string{"BRANCH", "STATUS", "DETAIL"}
	var rows [][]string

	for _, r := range results {
		var statusStr string
		switch r.Status {
		case prune.StatusDeleted, prune.StatusWorktreeRemoved:
			statusStr = pruneStyleGreen.Render(string(r.Status))
		case prune.StatusWouldDelete, prune.StatusWouldPrune, prune.StatusWorktreeWouldRemove:
			statusStr = pruneStyleYellow.Render(string(r.Status))
		case prune.StatusSkipped:
			statusStr = pruneStyleDim.Render(string(r.Status))
		case prune.StatusError:
			statusStr = pruneStyleRed.Render(string(r.Status))
		default:
			statusStr = string(r.Status)
		}

		detail := r.Detail
		if detail == "" {
			detail = "-"
		}

		rows = append(rows, []string{r.Branch, statusStr, detail})
	}

	return table.New().
		Border(lipgloss.ASCIIBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return pruneStyleHeader
			}
			return lipgloss.NewStyle()
		})
}
