package main

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"

	"github.com/obediencecorp/camp/internal/leverage"
	"github.com/obediencecorp/camp/internal/ui"
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
  camp leverage                              Show team leverage (auto-detect authors from git)
  camp leverage --author lance@example.com   Show personal leverage
  camp leverage --project camp               Show score for specific project
  camp leverage --json                       Output as JSON
  camp leverage --people 2                   Override team size
  camp leverage --verbose                    Show diagnostic details`,
	RunE: runLeverage,
}

func init() {
	leverageCmd.Flags().Bool("json", false, "output as JSON")
	leverageCmd.Flags().StringP("project", "p", "", "filter by project name")
	leverageCmd.Flags().Int("people", 0, "override team size (0 = auto-detect from git)")
	leverageCmd.Flags().Bool("no-legend", false, "hide the leverage formula legend")
	leverageCmd.Flags().BoolP("verbose", "v", false, "show diagnostic details (config, project resolution, exclusions)")
	leverageCmd.Flags().String("author", "", "filter by author email (git substring match — 'alice@co' matches 'alice@co.com')")
	rootCmd.AddCommand(leverageCmd)
	leverageCmd.GroupID = "campaign"
}

func runLeverage(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()

	// Parse flags
	jsonOut, _ := cmd.Flags().GetBool("json")
	projectFilter, _ := cmd.Flags().GetString("project")
	peopleOverride, _ := cmd.Flags().GetInt("people")
	verbose, _ := cmd.Flags().GetBool("verbose")
	authorFilter, _ := cmd.Flags().GetString("author")

	setup, err := initLeverageSetup(ctx)
	if err != nil {
		return err
	}
	cfg := setup.Cfg

	// Apply people override if specified
	if peopleOverride > 0 {
		cfg.ActualPeople = peopleOverride
	}

	// Default author from config if --author not set
	if authorFilter == "" && cfg.AuthorEmail != "" {
		authorFilter = cfg.AuthorEmail
	}

	runner, err := initRunner(cfg)
	if err != nil {
		return err
	}

	resolved, authorExcluded, err := resolveAndPopulateProjects(ctx, setup.Root, cfg, authorFilter)
	if err != nil {
		return err
	}

	// Verbose: show config and project resolution details
	if verbose {
		printVerboseLeverageInfo(cmd, cfg, setup, resolved)
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

		result, err := runner.Run(ctx, proj.SCCDir, proj.ExcludeDirs)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Warning: skipping %s: %v\n", proj.Name, err)
			continue
		}

		// Determine actual person-months for this project.
		// Use contribution-based actual PM (sum of each author's active duration)
		// rather than naive numAuthors * totalElapsed.
		var projActualPM float64
		var projPeople int
		var projElapsed float64

		if authorFilter != "" {
			// Personal leverage: 1 person, author-specific date range
			projPeople = 1
			first, last, gitErr := leverage.AuthorDateRange(ctx, proj.GitDir, authorFilter)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = 0.1 // minimum for single-commit authors
			}
			projActualPM = projElapsed
		} else if peopleOverride > 0 {
			// Manual override: use specified people count with project elapsed
			projPeople = peopleOverride
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
			projActualPM = float64(projPeople) * projElapsed
		} else if proj.ActualPersonMonths > 0 {
			// Auto-detect: use git-derived actual person-months
			projActualPM = proj.ActualPersonMonths
			projPeople = proj.AuthorCount
			if projPeople == 0 {
				projPeople = 1
			}
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
		} else {
			// Fallback
			projPeople = proj.AuthorCount
			if projPeople == 0 {
				projPeople = 1
			}
			first, last, gitErr := leverage.GitDateRange(ctx, proj.GitDir)
			if gitErr == nil {
				projElapsed = leverage.ElapsedMonths(first, last)
			}
			if projElapsed <= 0 {
				projElapsed = elapsed
			}
			projActualPM = float64(projPeople) * projElapsed
		}

		score := leverage.ComputeScore(result, projPeople, projElapsed)
		score.ProjectName = proj.Name
		score.AuthorCount = proj.AuthorCount

		// Override with contribution-based actual person-months
		if projActualPM > 0 {
			score.ActualPersonMonths = projActualPM
			estPM := result.EstimatedPeople * result.EstimatedScheduleMonths
			score.FullLeverage = estPM / projActualPM
		}

		scores = append(scores, score)
	}

	// Check if we filtered to a non-existent project
	if projectFilter != "" && len(scores) == 0 {
		return fmt.Errorf("project not found: %s", projectFilter)
	}

	// Determine effective team size for aggregate calculations.
	// When cfg.ActualPeople == 0 (auto-detect), use max author count from scores.
	effectivePeople := cfg.ActualPeople
	if effectivePeople == 0 {
		for _, s := range scores {
			if s.AuthorCount > effectivePeople {
				effectivePeople = s.AuthorCount
			}
		}
		if effectivePeople == 0 {
			effectivePeople = 1
		}
	}

	// Aggregate campaign-wide totals
	agg := leverage.AggregateScores(scores, effectivePeople, elapsed)

	// Override with deduplicated campaign-wide actual person-months.
	// AggregateScores naively sums per-project ActualPersonMonths, which
	// double-counts authors who contribute across multiple repos.
	// CampaignActualPersonMonths merges authors by name across all git dirs.
	if authorFilter == "" && peopleOverride == 0 {
		campaignPM, pmErr := leverage.CampaignActualPersonMonths(ctx, resolved)
		if pmErr == nil && campaignPM > 0 {
			estPM := agg.EstimatedPeople * agg.EstimatedMonths
			agg.ActualPersonMonths = campaignPM
			agg.FullLeverage = estPM / campaignPM
		}
	}

	// Compute recent leverage from snapshots
	store := leverage.NewFileSnapshotStore(leverage.DefaultSnapshotDir(setup.Root))
	week7, has7 := leverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -7))
	month30, has30 := leverage.RecentLeverage(ctx, store, scores, effectivePeople, now.AddDate(0, 0, -30))

	// Output based on format
	if jsonOut {
		return leverageOutputJSON(cmd, agg, scores)
	}

	recent := recentLeverage{week7: week7, has7: has7, month30: month30, has30: has30}
	opts := leverageOutputOpts{
		authorFilter:   authorFilter,
		authorExcluded: authorExcluded,
	}
	return leverageOutputTable(cmd, agg, scores, cfg, setup.AutoDetected, recent, opts)
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

// leverageOutputOpts holds display options for the table output.
type leverageOutputOpts struct {
	authorFilter   string
	authorExcluded int
}

func leverageOutputTable(cmd *cobra.Command, agg *leverage.LeverageScore, scores []*leverage.LeverageScore, cfg *leverage.LeverageConfig, autoDetected bool, recent recentLeverage, opts leverageOutputOpts) error {
	out := cmd.OutOrStdout()
	noLegend, _ := cmd.Flags().GetBool("no-legend")

	if autoDetected {
		fmt.Fprintln(out, ui.Warning("Note: Using auto-detected configuration. Run 'camp leverage config' to customize."))
		fmt.Fprintln(out)
	}

	// Header: headline leverage number
	if opts.authorFilter != "" {
		fmt.Fprintf(out, "%s %s  %s\n\n",
			ui.Header("Your Leverage:"),
			ui.Value(fmtScore(agg.FullLeverage)+"x", ui.AccentColor),
			ui.Dim("("+opts.authorFilter+")"))
	} else {
		authorInfo := ""
		if agg.AuthorCount > 0 {
			authorInfo = fmt.Sprintf("  %s", ui.Dim(fmt.Sprintf("(%d %s detected)",
				agg.AuthorCount, pluralize(agg.AuthorCount, "author", "authors"))))
		}
		fmt.Fprintf(out, "%s %s%s\n\n",
			ui.Header("Campaign Leverage:"),
			ui.Value(fmtScore(agg.FullLeverage)+"x", ui.AccentColor),
			authorInfo)
	}

	// Recent leverage from snapshots (omitted if no data)
	if recent.has7 || recent.has30 {
		if recent.has7 {
			fmt.Fprintf(out, "  %s %s\n",
				ui.Label("Last 7 days:"),
				ui.Value(fmtRecentLeverage(recent.week7)+"x", ui.SuccessColor))
		}
		if recent.has30 {
			fmt.Fprintf(out, "  %s %s\n",
				ui.Label("Last 30 days:"),
				ui.Value(fmtRecentLeverage(recent.month30)+"x", ui.SuccessColor))
		}
		fmt.Fprintf(out, "  %s\n", ui.Dim("(new estimated effort added in period vs actual effort spent)"))
		fmt.Fprintln(out)
	}

	// COCOMO vs Actual comparison in person-months
	estPersonMonths := agg.EstimatedPeople * agg.EstimatedMonths
	actualPersonMonths := agg.ActualPersonMonths
	if actualPersonMonths == 0 {
		actualPersonMonths = agg.ActualPeople * agg.ElapsedMonths
	}
	fmt.Fprintf(out, "  %s %s  %s\n",
		ui.Label("COCOMO Estimate:"),
		ui.Value(fmtInt(int(estPersonMonths))+" person-months"),
		ui.Value("($"+fmtCost(agg.EstimatedCost)+")", ui.WarningColor))

	if opts.authorFilter != "" {
		fmt.Fprintf(out, "  %s %s\n",
			ui.Label("Your Effort:"),
			ui.Value(fmt.Sprintf("%.1f person-months", actualPersonMonths)))
	} else {
		fmt.Fprintf(out, "  %s %s\n",
			ui.Label("Actual Effort:"),
			ui.Value(fmt.Sprintf("%.1f person-months", actualPersonMonths)))
	}
	fmt.Fprintf(out, "  %s %s\n\n",
		ui.Label("Team Equivalent:"),
		ui.Value(fmtScore(agg.SimpleLeverage)+"x", ui.AccentColor))

	// Summary line
	summaryParts := fmt.Sprintf("%s lines of code across %d %s",
		fmtInt(agg.TotalCode), len(scores), pluralize(len(scores), "project", "projects"))
	if opts.authorExcluded > 0 {
		summaryParts += fmt.Sprintf(" (%d excluded — no commits)", opts.authorExcluded)
	}
	fmt.Fprintf(out, "  %s\n", ui.Dim(summaryParts))
	fmt.Fprintf(out, "  %s\n", ui.Dim("Since "+cfg.ProjectStart.Format("Jan 2, 2006")))
	fmt.Fprintln(out)

	// Project table
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	headers := []string{"PROJECT", "FILES", "CODE", "AUTHORS", "EST COST", "EST PM", "ACTUAL PM", "LEVERAGE"}
	rows := buildScoreRows(scores)

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
			case 0: // PROJECT
				return lipgloss.NewStyle().Foreground(ui.AccentColor)
			case 4: // EST COST
				return lipgloss.NewStyle().Foreground(ui.WarningColor)
			case 7: // LEVERAGE
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)

	if !noLegend {
		fmt.Fprintln(out)
		fmt.Fprintln(out, ui.Dim("Leverage = estimated effort / actual effort (COCOMO organic model via scc)"))
		fmt.Fprintln(out, ui.Dim("Scores are for personal tracking — they vary widely by project type, language,"))
		fmt.Fprintln(out, ui.Dim("and team. Use them to measure your own trends, not to compare across teams."))
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
