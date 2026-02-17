package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/obediencecorp/camp/internal/ui"
)

var leverageHistoryCmd = &cobra.Command{
	Use:   "history",
	Short: "Show leverage score history over time",
	Long: `Display leverage data aggregated over time from stored snapshots.

Shows how leverage has changed week by week. Use --by-author to see
per-contributor leverage breakdown based on git blame attribution.

Requires snapshot data from 'camp leverage backfill' or 'camp leverage snapshot'.

Examples:
  camp leverage history                            Show all history
  camp leverage history --project camp             Filter to one project
  camp leverage history --since 2025-06-01         Start from June 2025
  camp leverage history --json                     Output as JSON
  camp leverage history --by-author                Per-author breakdown`,
	RunE: runLeverageHistory,
}

func init() {
	leverageHistoryCmd.Flags().StringP("project", "p", "", "filter to specific project")
	leverageHistoryCmd.Flags().String("since", "", "start date (YYYY-MM-DD)")
	leverageHistoryCmd.Flags().String("until", "", "end date (YYYY-MM-DD, default: today)")
	leverageHistoryCmd.Flags().String("period", "monthly", "aggregation period: weekly or monthly")
	leverageHistoryCmd.Flags().Bool("json", false, "output as JSON")
	leverageHistoryCmd.Flags().Bool("by-author", false, "show per-author leverage breakdown")
	leverageCmd.AddCommand(leverageHistoryCmd)
}

func runLeverageHistory(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	cfg := setup.Cfg

	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(setup.Root))

	// Determine project list
	projectFilter, _ := cmd.Flags().GetString("project")
	var projectNames []string
	if projectFilter != "" {
		projectNames = []string{projectFilter}
	} else {
		projectNames, err = store.ListProjects(ctx)
		if err != nil {
			return fmt.Errorf("listing projects: %w", err)
		}
	}

	if len(projectNames) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("No snapshots found. Run `camp leverage backfill` to populate historical data."))
		return nil
	}

	// Parse date range
	since := cfg.ProjectStart
	sinceStr, _ := cmd.Flags().GetString("since")
	if sinceStr != "" {
		since, err = time.Parse("2006-01-02", sinceStr)
		if err != nil {
			return fmt.Errorf("invalid --since date %q (use YYYY-MM-DD): %w", sinceStr, err)
		}
	}

	until := time.Now()
	untilStr, _ := cmd.Flags().GetString("until")
	if untilStr != "" {
		until, err = time.Parse("2006-01-02", untilStr)
		if err != nil {
			return fmt.Errorf("invalid --until date %q (use YYYY-MM-DD): %w", untilStr, err)
		}
	}

	// Determine period mode
	periodStr, _ := cmd.Flags().GetString("period")
	period := leverage.PeriodMonthly
	if periodStr == "weekly" {
		period = leverage.PeriodWeekly
	}

	// Determine actual people for history calculations.
	// When cfg.ActualPeople == 0 (auto-detect), resolve projects to get author counts.
	historyPeople := cfg.ActualPeople
	if historyPeople == 0 {
		resolved, resolveErr := leverage.ResolveProjects(ctx, setup.Root, cfg)
		if resolveErr == nil {
			for _, proj := range resolved {
				count, gitErr := leverage.CountAuthors(ctx, proj.GitDir)
				if gitErr == nil && count > historyPeople {
					historyPeople = count
				}
			}
		}
		if historyPeople == 0 {
			historyPeople = 1
		}
	}

	// Load history with period-based deltas
	history, err := leverage.LoadPeriodHistory(ctx, store, projectNames, historyPeople, since, until, period)
	if err != nil {
		return err
	}

	if len(history) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), ui.Dim("No snapshots found. Run `camp leverage backfill` to populate historical data."))
		return nil
	}

	// Output
	jsonFlag, _ := cmd.Flags().GetBool("json")
	byAuthor, _ := cmd.Flags().GetBool("by-author")

	if jsonFlag {
		return historyOutputJSON(cmd, history)
	}
	if byAuthor {
		return historyOutputByAuthor(cmd, history)
	}
	return historyOutputPeriodTable(cmd, history, period)
}

func historyOutputTable(cmd *cobra.Command, history []leverage.HistoryPoint) error {
	out := cmd.OutOrStdout()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	headers := []string{"DATE", "CODE LINES", "EST. COST", "LEVERAGE"}
	var rows [][]string
	for _, point := range history {
		lev := "-"
		if point.Aggregate != nil {
			lev = fmtScore(point.Aggregate.FullLeverage) + "x"
		}
		rows = append(rows, []string{
			point.Date.Format("2006-01-02"),
			fmtInt(point.TotalCode),
			"$" + fmtCost(point.TotalCost),
			lev,
		})
	}

	t := table.New().
		Border(lipgloss.ASCIIBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0: // DATE
				return lipgloss.NewStyle().Foreground(ui.DimColor)
			case 2: // EST. COST
				return lipgloss.NewStyle().Foreground(ui.WarningColor)
			case 3: // LEVERAGE
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)
	return nil
}

func historyOutputPeriodTable(cmd *cobra.Command, history []leverage.HistoryPoint, period leverage.HistoryPeriod) error {
	out := cmd.OutOrStdout()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	dateHeader := "MONTH"
	dateFmt := "2006-01"
	if period == leverage.PeriodWeekly {
		dateHeader = "DATE"
		dateFmt = "2006-01-02"
	}

	headers := []string{dateHeader, "Δ CODE", "Δ EST. COST", "LEVERAGE"}
	var rows [][]string
	for _, point := range history {
		lev := "-"
		if point.IsFirst {
			lev = "-"
		} else if point.IsNegative {
			lev = "negative"
		} else if point.PeriodLeverage > 0 {
			lev = fmtScore(point.PeriodLeverage) + "x"
		} else {
			lev = "-"
		}

		deltaCode := fmtDelta(point.DeltaCode)
		deltaCost := fmtDeltaCost(point.DeltaEstCost)
		if point.IsFirst {
			deltaCode = "-"
			deltaCost = "-"
		}
		if !point.IsFirst && point.DeltaCode == 0 && point.DeltaEstCost == 0 {
			lev = "-"
		}

		rows = append(rows, []string{
			point.Date.Format(dateFmt),
			deltaCode,
			deltaCost,
			lev,
		})
	}

	t := table.New().
		Border(lipgloss.ASCIIBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0: // DATE/MONTH
				return lipgloss.NewStyle().Foreground(ui.DimColor)
			case 1: // Δ CODE
				return lipgloss.NewStyle().Foreground(ui.AccentColor)
			case 2: // Δ EST. COST
				return lipgloss.NewStyle().Foreground(ui.WarningColor)
			case 3: // LEVERAGE
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)
	return nil
}

// fmtDelta formats an integer delta with +/- prefix.
func fmtDelta(n int) string {
	if n > 0 {
		return "+" + fmtInt(n)
	}
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	return "-"
}

// fmtDeltaCost formats a cost delta with +$/-$ prefix.
func fmtDeltaCost(f float64) string {
	if f > 0 {
		return "+$" + fmtCost(f)
	}
	if f < 0 {
		return "-$" + fmtCost(-f)
	}
	return "-"
}

func historyOutputByAuthor(cmd *cobra.Command, history []leverage.HistoryPoint) error {
	out := cmd.OutOrStdout()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	headers := []string{"DATE", "AUTHOR", "LINES OWNED", "OWNERSHIP %", "AUTHOR LEVERAGE"}
	var rows [][]string
	for _, point := range history {
		authors := aggregateAuthors(point.Projects)
		for _, author := range authors {
			authorLev := "-"
			if point.Aggregate != nil {
				authorLev = fmtScore(author.Percentage/100.0*point.Aggregate.FullLeverage) + "x"
			}
			rows = append(rows, []string{
				point.Date.Format("2006-01-02"),
				author.Name,
				fmtInt(author.Lines),
				fmt.Sprintf("%.1f%%", author.Percentage),
				authorLev,
			})
		}
	}

	t := table.New().
		Border(lipgloss.ASCIIBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0: // DATE
				return lipgloss.NewStyle().Foreground(ui.DimColor)
			case 1: // AUTHOR
				return lipgloss.NewStyle().Foreground(ui.AccentColor)
			case 4: // AUTHOR LEVERAGE
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)
	return nil
}

func historyOutputJSON(cmd *cobra.Command, history []leverage.HistoryPoint) error {
	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// aggregateAuthors combines author contributions across all projects at a point in time.
func aggregateAuthors(projects map[string]*leverage.Snapshot) []leverage.AuthorContribution {
	byEmail := make(map[string]*leverage.AuthorContribution)
	var totalLines int

	for _, snap := range projects {
		for _, a := range snap.Authors {
			totalLines += a.Lines
			if existing, ok := byEmail[a.Email]; ok {
				existing.Lines += a.Lines
			} else {
				copy := a
				byEmail[a.Email] = &copy
			}
		}
	}

	result := make([]leverage.AuthorContribution, 0, len(byEmail))
	for _, a := range byEmail {
		if totalLines > 0 {
			a.Percentage = float64(a.Lines) / float64(totalLines) * 100
		}
		result = append(result, *a)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Lines > result[j].Lines
	})
	return result
}
