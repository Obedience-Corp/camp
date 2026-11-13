package project

import (
	"context"
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

A merged branch's worktree is removed with it. When that worktree holds
uncommitted edits, they are saved first and the command prints how to restore
them; saved edits are dropped after 30 days. The worktree is kept instead when
it is locked, was edited in the last 24 hours, has a merge or rebase in
progress, holds more than 50 MiB of uncommitted files, or has changes inside a
submodule that a save could not capture.

A branch whose upstream is gone is treated as merged when its commits match a
commit on the default branch, or when GitHub (through the gh CLI) reports a
merged pull request whose head is the branch's local tip.

Examples:
  camp project prune                     # Prune current project
  camp project prune camp                # Prune by name
  camp project prune -p camp             # Prune by flag
  camp project prune --dry-run           # Preview what would be deleted
	camp project prune --remote            # Also prune stale remote tracking refs
  camp project prune --remote-delete     # Also delete merged branches on origin
  camp project prune --keep-dirty        # Never remove a worktree with uncommitted edits
  camp project prune --saved             # List saved uncommitted edits
  camp project prune --restore my-branch # Recreate a removed worktree with its saved edits
  camp project prune --drop my-branch    # Delete saved edits now`,
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
	pruneDiscardDirty bool
	pruneKeepDirty    bool
	pruneListSaved    bool
	pruneRestore      string
	pruneDrop         string
)

func init() {
	projectPruneCmd.Flags().StringVarP(&pruneProjectFlag, "project", "p", "", "Project name (auto-detected from cwd)")
	// Persistent so 'camp project prune all' accepts them too.
	projectPruneCmd.PersistentFlags().BoolVarP(&pruneDryRun, "dry-run", "n", false, "Preview without deleting")
	projectPruneCmd.PersistentFlags().BoolVarP(&pruneForce, "force", "f", false, "Skip local branch deletion confirmation")
	projectPruneCmd.PersistentFlags().BoolVar(&pruneRemote, "remote", false, "Also prune stale remote tracking refs")
	projectPruneCmd.PersistentFlags().BoolVar(&pruneRemoteDelete, "remote-delete", false, "Also delete merged branches on origin (destructive)")
	projectPruneCmd.PersistentFlags().BoolVar(&pruneDiscardDirty, "discard-dirty", false, "Remove worktrees with uncommitted changes without saving them")
	projectPruneCmd.PersistentFlags().BoolVar(&pruneKeepDirty, "keep-dirty", false, "Keep every worktree that has uncommitted changes")
	projectPruneCmd.Flags().BoolVar(&pruneListSaved, "saved", false, "List uncommitted edits saved from removed worktrees")
	projectPruneCmd.Flags().StringVar(&pruneRestore, "restore", "", "Recreate the removed worktree for this branch with its saved edits")
	projectPruneCmd.Flags().StringVar(&pruneDrop, "drop", "", "Delete the saved edits for this branch")
	projectPruneCmd.MarkFlagsMutuallyExclusive("saved", "restore", "drop")
	projectPruneCmd.MarkFlagsMutuallyExclusive("discard-dirty", "keep-dirty")

	if err := projectPruneCmd.RegisterFlagCompletionFunc("project", cmdutil.CompleteProjectName); err != nil {
		panic(err)
	}

	Cmd.AddCommand(projectPruneCmd)
}

// pruneOptionsFromFlags constructs prune.Options from the package-level flag vars.
//
// RefreshRemote is on for both dry-run and non-dry-run: 'git fetch --prune'
// only updates remote-tracking refs, not the worktree, so dry-run needs it
// to preview the current prune state accurately.
func pruneOptionsFromFlags() prune.Options {
	return prune.Options{
		DryRun:        pruneDryRun,
		Force:         pruneForce,
		Remote:        pruneRemote,
		RemoteDelete:  pruneRemoteDelete,
		DiscardDirty:  pruneDiscardDirty,
		SaveDirty:     !pruneKeepDirty,
		MergedPRs:     prune.GitHubMergedPRHeads,
		RefreshRemote: true,
	}
}

func runProjectPrune(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	campRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return camperrors.Wrap(err, "not in a camp")
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

	switch {
	case pruneListSaved:
		return renderSavedEdits(ctx, result.Name, result.Path)
	case pruneRestore != "":
		saved, err := prune.RestoreSaved(ctx, result.Path, pruneRestore)
		if err != nil {
			return camperrors.Wrapf(err, "restore saved edits for %s", pruneRestore)
		}
		fmt.Printf("%s Restored %s with its uncommitted edits at %s\n", ui.SuccessIcon(), ui.Value(saved.Branch), saved.Worktree)
		return nil
	case pruneDrop != "":
		saved, err := prune.DropSaved(ctx, result.Path, pruneDrop)
		if err != nil {
			return camperrors.Wrapf(err, "drop saved edits for %s", pruneDrop)
		}
		fmt.Printf("%s Dropped the saved edits for %s\n", ui.SuccessIcon(), ui.Value(saved.Branch))
		return nil
	}

	pr := prune.Execute(ctx, result.Name, result.Path, pruneOptionsFromFlags())

	renderPruneResult(pr)

	return nil
}

func renderSavedEdits(ctx context.Context, name, path string) error {
	saved, err := prune.ListSaved(ctx, path)
	if err != nil {
		return err
	}
	if len(saved) == 0 {
		fmt.Printf("%s %s: %s\n", ui.SuccessIcon(), ui.Value(name), ui.Dim("no saved edits"))
		return nil
	}
	rows := make([][]string, 0, len(saved))
	for _, s := range saved {
		rows = append(rows, []string{s.Branch, formatSavedBytes(s.Bytes),
			s.SavedAt.Format("2006-01-02"), s.Expires.Format("2006-01-02"), s.Worktree})
	}
	fmt.Printf("\n%s %s\n", ui.Subheader("Saved edits:"), ui.Value(name))
	fmt.Println(pruneTable([]string{"BRANCH", "SIZE", "SAVED", "EXPIRES", "WORKTREE"}, rows))
	fmt.Println(ui.Dim("Restore one with --restore <branch>, delete one with --drop <branch>."))
	return nil
}

func formatSavedBytes(n int64) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.1f KiB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
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

	defer func() {
		for _, note := range pr.Notes {
			fmt.Printf("%s %s\n", ui.WarningIcon(), note)
		}
	}()

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
		case prune.StatusDeleted, prune.StatusWorktreeRemoved, prune.StatusEditsSaved:
			statusStr = pruneStyleGreen.Render(string(r.Status))
		case prune.StatusWouldDelete, prune.StatusWouldPrune, prune.StatusWorktreeWouldRemove, prune.StatusEditsWouldSave:
			statusStr = pruneStyleYellow.Render(string(r.Status))
		case prune.StatusSkipped, prune.StatusSavedExpired:
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

	return pruneTable(headers, rows)
}

func pruneTable(headers []string, rows [][]string) *table.Table {
	return table.New().
		Border(lipgloss.RoundedBorder()).
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
