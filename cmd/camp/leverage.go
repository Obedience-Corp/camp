package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/spf13/cobra"
)

// sccRunner is the package-level runner used by the leverage command.
// Tests can replace this to inject a mock.
var sccRunner leverage.Runner

var leverageCmd = &cobra.Command{
	Use:   "leverage",
	Short: "Compute leverage scores for campaign projects",
	Long: `Compute productivity leverage scores by comparing scc COCOMO estimates
against actual development effort.

Leverage score measures how much more output you produce versus what
traditional estimation models predict for the same team and time.

  FullLeverage   = (EstimatedPeople x EstimatedMonths) / (ActualPeople x ElapsedMonths)
  SimpleLeverage = EstimatedPeople / ActualPeople

Examples:
  camp leverage                     Show all project scores
  camp leverage --project camp      Show score for specific project
  camp leverage --json              Output as JSON
  camp leverage --people 2          Override team size`,
	RunE: runLeverage,
}

func init() {
	leverageCmd.Flags().Bool("json", false, "output as JSON")
	leverageCmd.Flags().StringP("project", "p", "", "filter by project name")
	leverageCmd.Flags().Int("people", 0, "override team size (0 = use config)")
	leverageCmd.Flags().Bool("no-legend", false, "hide the leverage formula legend")
	rootCmd.AddCommand(leverageCmd)
	leverageCmd.GroupID = "campaign"
}

func runLeverage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	jsonOut, _ := cmd.Flags().GetBool("json")
	projectFilter, _ := cmd.Flags().GetString("project")
	peopleOverride, _ := cmd.Flags().GetInt("people")

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	cfg := setup.Cfg

	// Apply people override if specified
	if peopleOverride > 0 {
		cfg.ActualPeople = peopleOverride
	}

	runner, err := initRunner(cfg)
	if err != nil {
		return err
	}

	resolved, err := leverage.ResolveProjects(ctx, setup.Root, cfg)
	if err != nil {
		return fmt.Errorf("resolving projects: %w", err)
	}

	// Compute elapsed months
	now := time.Now()
	elapsed := leverage.ElapsedMonths(cfg.ProjectStart, now)

	// Run scc and compute scores for each project
	var scores []*leverage.LeverageScore
	for _, proj := range resolved {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if projectFilter != "" && proj.Name != projectFilter {
			continue
		}

		result, err := runner.Run(ctx, proj.SCCDir)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", proj.Name, err)
			continue
		}

		// Per-project elapsed from git history (first commit → last commit).
		projElapsed := elapsed
		first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
		if gitErr == nil {
			projElapsed = leverage.ElapsedMonths(first, last)
			if projElapsed <= 0 {
				projElapsed = elapsed // single-commit projects fall back to campaign elapsed
			}
		}

		score := leverage.ComputeScore(result, cfg.ActualPeople, projElapsed)
		score.ProjectName = proj.Name
		scores = append(scores, score)
	}

	// Check if we filtered to a non-existent project
	if projectFilter != "" && len(scores) == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	// Aggregate campaign-wide totals
	agg := leverage.AggregateScores(scores, cfg.ActualPeople, elapsed)

	// Compute recent leverage from snapshots
	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(setup.Root))
	week7, has7 := leverage.RecentLeverage(ctx, store, scores, cfg.ActualPeople, now.AddDate(0, 0, -7))
	month30, has30 := leverage.RecentLeverage(ctx, store, scores, cfg.ActualPeople, now.AddDate(0, 0, -30))

	// Output based on format
	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}

	recent := recentLeverage{week7: week7, has7: has7, month30: month30, has30: has30}
	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recent)
}

func leverageOutputJSON(cmd *cobra.Command, agg *leverage.LeverageScore, scores []*leverage.LeverageScore) error {
	output := struct {
		Campaign *leverage.LeverageScore   `json:"campaign"`
		Projects []*leverage.LeverageScore `json:"projects"`
	}{
		Campaign: agg,
		Projects: scores,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling JSON: %w", err)
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

// recentLeverage holds optional 7-day and 30-day leverage computed from snapshots.
type recentLeverage struct {
	week7, month30 float64
	has7, has30    bool
}

func leverageOutputTable(cmd *cobra.Command, agg *leverage.LeverageScore, scores []*leverage.LeverageScore, cfg *leverage.LeverageConfig, autoDetected bool, recent recentLeverage) error {
	out := cmd.OutOrStdout()
	noLegend, _ := cmd.Flags().GetBool("no-legend")

	if autoDetected {
		fmt.Fprintln(out, "Note: Using auto-detected configuration. Run 'camp leverage config' to customize.")
		fmt.Fprintln(out)
	}

	// Header: headline leverage number
	fmt.Fprintf(out, "Campaign Leverage: %sx\n\n", fmtScore(agg.FullLeverage))

	// Recent leverage from snapshots (omitted if no data)
	if recent.has7 || recent.has30 {
		if recent.has7 {
			fmt.Fprintf(out, "  Last 7 days:   %sx\n", fmtRecentLeverage(recent.week7))
		}
		if recent.has30 {
			fmt.Fprintf(out, "  Last 30 days:  %sx\n", fmtRecentLeverage(recent.month30))
		}
		fmt.Fprintln(out)
	}

	// COCOMO vs Actual comparison in person-months (the unit that sums correctly)
	estPersonMonths := agg.EstimatedPeople * agg.EstimatedMonths
	actualPersonMonths := agg.ActualPeople * agg.ElapsedMonths
	fmt.Fprintf(out, "  COCOMO Estimate:  %s person-months  ($%s)\n",
		fmtInt(int(estPersonMonths)), fmtCost(agg.EstimatedCost))
	fmt.Fprintf(out, "  Actual Effort:    %.1f person-months\n", actualPersonMonths)
	fmt.Fprintf(out, "  Team Equivalent:  %sx\n\n", fmtScore(agg.SimpleLeverage))

	// Summary line
	fmt.Fprintf(out, "  %s lines of code across %d %s\n",
		fmtInt(agg.TotalCode), len(scores), pluralize(len(scores), "project", "projects"))
	fmt.Fprintf(out, "  Since %s\n", cfg.ProjectStart.Format("Jan 2, 2006"))
	fmt.Fprintln(out)

	// Project table
	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tFILES\tCODE\tEST COST\tEST PERSON-MONTHS\tACTUAL MONTHS\tLEVERAGE")
	fmt.Fprintln(w, "-------\t-----\t----\t--------\t-----------------\t-------------\t--------")

	for _, s := range scores {
		estPM := s.EstimatedPeople * s.EstimatedMonths
		fmt.Fprintf(w, "%s\t%s\t%s\t$%s\t%s\t%.3f\t%sx\n",
			s.ProjectName,
			fmtInt(s.TotalFiles),
			fmtInt(s.TotalCode),
			fmtCost(s.EstimatedCost),
			fmtInt(int(estPM)),
			s.ElapsedMonths,
			fmtScore(s.FullLeverage),
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if !noLegend {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Leverage = estimated effort / actual effort (COCOMO organic model via scc)")
	}
	return nil
}

// fmtInt formats an integer with comma separators (e.g., 805433 → "805,433").
func fmtInt(n int) string {
	if n < 0 {
		return "-" + fmtInt(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result []byte
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			result = append(result, ',')
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// fmtCost formats a float64 cost with comma separators (e.g., 28218013.0 → "28,218,013").
func fmtCost(f float64) string {
	return fmtInt(int(f))
}

// fmtRecentLeverage formats a recent period leverage value.
// Handles negative leverage (code removal) and zero.
func fmtRecentLeverage(f float64) string {
	if f < 0 {
		return fmt.Sprintf("%.1f (negative)", f)
	}
	return fmtScore(f)
}

// fmtScore formats a leverage score, using commas for large values.
func fmtScore(f float64) string {
	if f >= 1000 {
		return fmt.Sprintf("%s.%d", fmtInt(int(f)), int(f*10)%10)
	}
	return fmt.Sprintf("%.1f", f)
}

// pluralize returns singular if n == 1, plural otherwise.
func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
