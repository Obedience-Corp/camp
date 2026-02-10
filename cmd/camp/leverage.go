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

		score := leverage.ComputeScore(result, cfg.ActualPeople, elapsed)
		score.ProjectName = proj.Name
		scores = append(scores, score)
	}

	// Check if we filtered to a non-existent project
	if projectFilter != "" && len(scores) == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	// Aggregate campaign-wide totals
	agg := leverage.AggregateScores(scores, cfg.ActualPeople, elapsed)

	// Output based on format
	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}
	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected)
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

func leverageOutputTable(cmd *cobra.Command, agg *leverage.LeverageScore, scores []*leverage.LeverageScore, cfg *leverage.LeverageConfig, autoDetected bool) error {
	out := cmd.OutOrStdout()
	noLegend, _ := cmd.Flags().GetBool("no-legend")

	if autoDetected {
		fmt.Fprintln(out, "Note: Using auto-detected configuration. Run 'camp leverage config' to customize.")
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "Campaign Leverage Score\n")
	fmt.Fprintf(out, "  Effort:  %sx  (estimated_people x estimated_months) / (actual_people x elapsed_months)\n", fmtScore(agg.FullLeverage))
	fmt.Fprintf(out, "  Team:    %sx  estimated_people / actual_people\n\n", fmtScore(agg.SimpleLeverage))
	fmt.Fprintf(out, "Estimated: %.1f people x %.1f months | Actual: %d %s x %.1f months\n",
		agg.EstimatedPeople, agg.EstimatedMonths, cfg.ActualPeople, pluralize(cfg.ActualPeople, "person", "people"), agg.ElapsedMonths)
	fmt.Fprintf(out, "Total Code: %s lines | Estimated Cost: $%s\n", fmtInt(agg.TotalCode), fmtCost(agg.EstimatedCost))
	fmt.Fprintf(out, "Since: %s (earliest commit across all projects)\n", cfg.ProjectStart.Format("Jan 2, 2006"))
	fmt.Fprintln(out)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tCODE\tEST PEOPLE\tEFFORT\tTEAM")
	fmt.Fprintln(w, "-------\t----\t----------\t------\t----")

	for _, s := range scores {
		fmt.Fprintf(w, "%s\t%s\t%.1f\t%sx\t%sx\n",
			s.ProjectName,
			fmtInt(s.TotalCode),
			s.EstimatedPeople,
			fmtScore(s.FullLeverage),
			fmtScore(s.SimpleLeverage),
		)
	}
	if err := w.Flush(); err != nil {
		return err
	}

	if !noLegend {
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Effort = person-months leverage (how much faster and bigger than a traditional team)")
		fmt.Fprintln(out, "Team   = headcount leverage (equivalent team size for this output)")
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
