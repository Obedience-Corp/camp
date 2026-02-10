package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"text/tabwriter"
	"time"

	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/obediencecorp/camp/internal/project"
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
	rootCmd.AddCommand(leverageCmd)
	leverageCmd.GroupID = "campaign"
}

func runLeverage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	jsonOut, _ := cmd.Flags().GetBool("json")
	projectFilter, _ := cmd.Flags().GetString("project")
	peopleOverride, _ := cmd.Flags().GetInt("people")

	// Detect campaign root
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return fmt.Errorf("not in a campaign: %w", err)
	}

	// Load config; auto-detect if file doesn't exist
	configPath := leverage.DefaultConfigPath(root)
	cfg, err := leverage.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	// If config has no ProjectStart, auto-detect it
	autoDetected := cfg.ProjectStart.IsZero()
	if autoDetected {
		detected, err := leverage.AutoDetectConfig(ctx, root)
		if err != nil {
			return fmt.Errorf("auto-detecting config: %w", err)
		}
		cfg = detected
	}

	// Apply people override if specified
	if peopleOverride > 0 {
		cfg.ActualPeople = peopleOverride
	}

	// Create SCC runner (use injected runner if set, e.g. in tests)
	runner := sccRunner
	if runner == nil {
		r, err := leverage.NewSCCRunner(cfg.COCOMOProjectType)
		if err != nil {
			return err
		}
		runner = r
	}

	// Discover projects via project.List
	projects, err := project.List(ctx, root)
	if err != nil {
		return fmt.Errorf("listing projects: %w", err)
	}

	// Compute elapsed months
	now := time.Now()
	elapsed := leverage.ElapsedMonths(cfg.ProjectStart, now)

	// Run scc and compute scores for each project
	var scores []*leverage.LeverageScore
	for _, proj := range projects {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// Skip if filtering and doesn't match
		if projectFilter != "" && proj.Name != projectFilter {
			continue
		}

		// proj.Path is relative (e.g., "projects/camp"), make absolute
		absPath := filepath.Join(root, proj.Path)

		result, err := runner.Run(ctx, absPath)
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
	return leverageOutputTable(cmd, agg, scores, cfg, autoDetected)
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

	if autoDetected {
		fmt.Fprintln(out, "Note: Using auto-detected configuration. Run 'camp leverage config' to customize.")
		fmt.Fprintln(out)
	}

	fmt.Fprintf(out, "Campaign Leverage Score: %.1fx (full)  %.1fx (simple)\n", agg.FullLeverage, agg.SimpleLeverage)
	fmt.Fprintf(out, "Estimated: %.1f people x %.1f months | Actual: %d people x %.1f months\n",
		agg.EstimatedPeople, agg.EstimatedMonths, cfg.ActualPeople, agg.ElapsedMonths)
	fmt.Fprintf(out, "Total Code: %d lines | Estimated Cost: $%.0f\n\n", agg.TotalCode, agg.EstimatedCost)

	w := tabwriter.NewWriter(out, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "PROJECT\tCODE\tEST PEOPLE\tFULL LEV\tSIMPLE LEV")
	fmt.Fprintln(w, "-------\t----\t----------\t--------\t----------")

	for _, s := range scores {
		fmt.Fprintf(w, "%s\t%d\t%.1f\t%.1fx\t%.1fx\n",
			s.ProjectName,
			s.TotalCode,
			s.EstimatedPeople,
			s.FullLeverage,
			s.SimpleLeverage,
		)
	}
	return w.Flush()
}
