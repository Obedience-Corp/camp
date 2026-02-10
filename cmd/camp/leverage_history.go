package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"text/tabwriter"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/cobra"
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
	leverageHistoryCmd.Flags().Bool("json", false, "output as JSON")
	leverageHistoryCmd.Flags().Bool("by-author", false, "show per-author leverage breakdown")
	leverageCmd.AddCommand(leverageHistoryCmd)
}

func runLeverageHistory(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	configPath := leverage.DefaultConfigPath(root)
	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if cfg.ProjectStart.IsZero() {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return fmt.Errorf("auto-detecting config: %w", err)
		}
		cfg = detected
	}

	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(root))

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
		fmt.Fprintln(cmd.OutOrStdout(), "No snapshots found. Run `camp leverage backfill` to populate historical data.")
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

	// Load history
	history, err := leverage.LoadHistory(ctx, store, projectNames, cfg.ActualPeople, since, until)
	if err != nil {
		return err
	}

	if len(history) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No snapshots found. Run `camp leverage backfill` to populate historical data.")
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
	return historyOutputTable(cmd, history)
}

func historyOutputTable(cmd *cobra.Command, history []leverage.HistoryPoint) error {
	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tCODE LINES\tEST. COST\tLEVERAGE")
	fmt.Fprintln(w, "----\t----------\t---------\t--------")

	for _, point := range history {
		lev := "-"
		if point.Aggregate != nil {
			lev = fmtScore(point.Aggregate.FullLeverage) + "x"
		}
		fmt.Fprintf(w, "%s\t%s\t$%s\t%s\n",
			point.Date.Format("2006-01-02"),
			fmtInt(point.TotalCode),
			fmtCost(point.TotalCost),
			lev,
		)
	}
	return w.Flush()
}

func historyOutputByAuthor(cmd *cobra.Command, history []leverage.HistoryPoint) error {
	out := cmd.OutOrStdout()
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "DATE\tAUTHOR\tLINES OWNED\tOWNERSHIP %\tAUTHOR LEVERAGE")
	fmt.Fprintln(w, "----\t------\t-----------\t-----------\t---------------")

	for _, point := range history {
		authors := aggregateAuthors(point.Projects)
		for _, author := range authors {
			authorLev := "-"
			if point.Aggregate != nil {
				authorLev = fmtScore(author.Percentage/100.0*point.Aggregate.FullLeverage) + "x"
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%.1f%%\t%s\n",
				point.Date.Format("2006-01-02"),
				author.Name,
				fmtInt(author.Lines),
				author.Percentage,
				authorLev,
			)
		}
	}
	return w.Flush()
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
