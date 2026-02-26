package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

// pruneStatus represents the outcome of a single branch prune operation.
type pruneStatus string

const (
	pruneStatusDeleted     pruneStatus = "deleted"
	pruneStatusWouldDelete pruneStatus = "would delete"
	pruneStatusSkipped     pruneStatus = "skipped"
	pruneStatusError       pruneStatus = "error"
	pruneStatusWouldPrune  pruneStatus = "would prune"
)

// PruneOptions holds configuration for a prune operation.
type PruneOptions struct {
	DryRun       bool
	Force        bool
	Remote       bool
	RemoteDelete bool
}

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
	ValidArgsFunction: completeProjectName,
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
	projectPruneCmd.Flags().BoolVarP(&pruneForce, "force", "f", false, "Skip confirmation prompt for local deletion")
	projectPruneCmd.Flags().BoolVar(&pruneRemote, "remote", false, "Also prune stale remote tracking refs")
	projectPruneCmd.Flags().BoolVar(&pruneRemoteDelete, "remote-delete", false, "Also delete merged branches on origin (destructive)")

	projectPruneCmd.RegisterFlagCompletionFunc("project", completeProjectName)

	projectCmd.AddCommand(projectPruneCmd)
}

// pruneResult holds the outcome for a single branch.
type pruneResult struct {
	Branch string
	Status pruneStatus
	Detail string
}

// projectPruneResult holds all results for a single project.
type projectPruneResult struct {
	Name    string
	Path    string
	Results []pruneResult
	Pruned  int // remote refs pruned
	Error   string
}

// pruneOptionsFromFlags constructs PruneOptions from the package-level flag vars.
func pruneOptionsFromFlags() PruneOptions {
	return PruneOptions{
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
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Resolve project: positional arg > flag > cwd
	projectName := pruneProjectFlag
	if len(args) > 0 {
		projectName = args[0]
	}

	result, err := project.Resolve(ctx, campRoot, projectName)
	if err != nil {
		var notFound *project.ProjectNotFoundError
		if errors.As(err, &notFound) {
			fmt.Println(ui.Dim("\n" + project.FormatProjectList(notFound.AvailableProjects())))
		}
		return err
	}

	pr := executePrune(ctx, result.Name, result.Path, pruneOptionsFromFlags())

	renderPruneResult(pr)

	return nil
}

// executePrune runs the prune logic for a single project.
func executePrune(ctx context.Context, name, path string, opts PruneOptions) projectPruneResult {
	pr := projectPruneResult{Name: name, Path: path}

	merged, err := git.MergedBranches(ctx, path)
	if err != nil {
		pr.Error = err.Error()
		return pr
	}

	if len(merged) == 0 && !opts.Remote {
		return pr
	}

	// Confirmation for local deletion (unless dry-run or force)
	if len(merged) > 0 && !opts.DryRun && !opts.Force {
		fmt.Printf("\n%s Will delete %d merged branch(es) in %s:\n",
			ui.WarningIcon(), len(merged), ui.Value(name))
		for _, b := range merged {
			fmt.Printf("  %s %s\n", ui.Dim("-"), b)
		}
		fmt.Print("\nProceed? [y/N] ")
		var answer string
		fmt.Scanln(&answer)
		if !strings.HasPrefix(strings.ToLower(answer), "y") {
			for _, b := range merged {
				pr.Results = append(pr.Results, pruneResult{
					Branch: b,
					Status: pruneStatusSkipped,
					Detail: "cancelled by user",
				})
			}
			return pr
		}
	}

	// Delete merged branches locally
	for _, branch := range merged {
		if opts.DryRun {
			pr.Results = append(pr.Results, pruneResult{
				Branch: branch,
				Status: pruneStatusWouldDelete,
			})
			continue
		}

		if err := git.DeleteBranch(ctx, path, branch); err != nil {
			pr.Results = append(pr.Results, pruneResult{
				Branch: branch,
				Status: pruneStatusError,
				Detail: err.Error(),
			})
		} else {
			pr.Results = append(pr.Results, pruneResult{
				Branch: branch,
				Status: pruneStatusDeleted,
			})
		}
	}

	// Remote branch deletion — separate confirmation gate (irreversible)
	if opts.RemoteDelete && len(merged) > 0 {
		if opts.DryRun {
			for _, branch := range merged {
				pr.Results = append(pr.Results, pruneResult{
					Branch: "origin/" + branch,
					Status: pruneStatusWouldDelete,
					Detail: "remote",
				})
			}
		} else {
			// Always confirm remote deletion independently — --force only covers local
			fmt.Printf("\n%s Will DELETE %d branch(es) from origin (irreversible):\n",
				ui.WarningIcon(), len(merged))
			for _, b := range merged {
				fmt.Printf("  %s origin/%s\n", ui.Dim("-"), b)
			}
			fmt.Print("\nDelete from remote? [y/N] ")
			var answer string
			fmt.Scanln(&answer)
			if strings.HasPrefix(strings.ToLower(answer), "y") {
				for _, branch := range merged {
					if err := git.DeleteRemoteBranch(ctx, path, branch); err != nil {
						pr.Results = append(pr.Results, pruneResult{
							Branch: "origin/" + branch,
							Status: pruneStatusError,
							Detail: err.Error(),
						})
					} else {
						pr.Results = append(pr.Results, pruneResult{
							Branch: "origin/" + branch,
							Status: pruneStatusDeleted,
							Detail: "remote",
						})
					}
				}
			} else {
				for _, branch := range merged {
					pr.Results = append(pr.Results, pruneResult{
						Branch: "origin/" + branch,
						Status: pruneStatusSkipped,
						Detail: "remote deletion cancelled",
					})
				}
			}
		}
	}

	// Prune stale remote tracking refs
	if opts.Remote {
		if opts.DryRun {
			pr.Results = append(pr.Results, pruneResult{
				Branch: "(remote tracking refs)",
				Status: pruneStatusWouldPrune,
			})
		} else {
			count, err := git.PruneRemote(ctx, path)
			if err != nil {
				pr.Results = append(pr.Results, pruneResult{
					Branch: "(remote tracking refs)",
					Status: pruneStatusError,
					Detail: err.Error(),
				})
			} else {
				pr.Pruned = count
			}
		}
	}

	return pr
}

// Package-level styles for prune output — allocated once.
var (
	pruneStyleGreen  = lipgloss.NewStyle().Foreground(ui.SuccessColor)
	pruneStyleRed    = lipgloss.NewStyle().Foreground(ui.ErrorColor)
	pruneStyleYellow = lipgloss.NewStyle().Foreground(ui.WarningColor)
	pruneStyleDim    = lipgloss.NewStyle().Foreground(ui.DimColor)
	pruneStyleHeader = lipgloss.NewStyle().Bold(true).Foreground(ui.BrightColor)
)

func renderPruneResult(pr projectPruneResult) {
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
		headers := []string{"BRANCH", "STATUS", "DETAIL"}
		var rows [][]string

		for _, r := range pr.Results {
			var statusStr string
			switch r.Status {
			case pruneStatusDeleted:
				statusStr = pruneStyleGreen.Render(string(r.Status))
			case pruneStatusWouldDelete, pruneStatusWouldPrune:
				statusStr = pruneStyleYellow.Render(string(r.Status))
			case pruneStatusSkipped:
				statusStr = pruneStyleDim.Render(string(r.Status))
			case pruneStatusError:
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

		t := table.New().
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

		fmt.Println(t)
	}

	if pr.Pruned > 0 {
		fmt.Printf("%s Pruned %d stale remote tracking ref(s)\n", ui.SuccessIcon(), pr.Pruned)
	}

	// Summary line
	deleted := 0
	for _, r := range pr.Results {
		if r.Status == pruneStatusDeleted {
			deleted++
		}
	}
	if deleted > 0 {
		fmt.Printf("\n%s Pruned %d branch(es)\n", ui.SuccessIcon(), deleted)
	}
}
