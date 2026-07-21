package leverage

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
	"github.com/spf13/cobra"
)

type recentLeverage struct {
	week7, month30 float64
	has7, has30    bool
	needsBackfill  bool
}

type leverageOutputOpts struct {
	authorFilter   string
	authorExcluded int
	directoryMode  bool
	directoryName  string
}

func leverageOutputJSON(cmd *cobra.Command, agg *intleverage.LeverageScore, scores []*intleverage.LeverageScore) error {
	output := struct {
		Campaign *intleverage.LeverageScore   `json:"campaign"`
		Projects []*intleverage.LeverageScore `json:"projects"`
	}{
		Campaign: agg,
		Projects: scores,
	}

	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return camperrors.Wrap(err, "marshaling JSON")
	}
	fmt.Fprintln(cmd.OutOrStdout(), string(data))
	return nil
}

func leverageOutputTable(cmd *cobra.Command, agg *intleverage.LeverageScore, scores []*intleverage.LeverageScore, cfg *intleverage.LeverageConfig, autoDetected bool, recent recentLeverage, opts leverageOutputOpts) error {
	out := cmd.OutOrStdout()
	noLegend, _ := cmd.Flags().GetBool("no-legend")

	if autoDetected && !opts.directoryMode {
		fmt.Fprintln(out, ui.Warning("Note: Using auto-detected configuration. Run 'camp leverage config' to customize."))
		fmt.Fprintln(out)
	}

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)
	headers := []string{"PROJECT", "FILES", "CODE", "AUTHORS", "EST COST", "EST PM", "ACTUAL PM", "LEVERAGE"}
	rows := buildScoreRows(scores)

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0:
				return lipgloss.NewStyle().Foreground(ui.AccentColor)
			case 4:
				return lipgloss.NewStyle().Foreground(ui.WarningColor)
			case 7:
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

	fmt.Fprintln(out)

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
		label := "Campaign Leverage:"
		if opts.directoryMode {
			label = fmt.Sprintf("Directory Leverage (%s):", opts.directoryName)
		}
		fmt.Fprintf(out, "%s %s%s\n\n",
			ui.Header(label),
			ui.Value(fmtScore(agg.FullLeverage)+"x", ui.AccentColor),
			authorInfo)
	}

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
	} else if recent.needsBackfill {
		fmt.Fprintf(out, "  %s\n", ui.Dim("Recent leverage history unavailable yet. This run saved current snapshots; run `camp leverage backfill` once to seed Last 7/30 days."))
		fmt.Fprintln(out)
	}

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

	summaryParts := fmt.Sprintf("%s lines of code across %d %s",
		fmtInt(agg.TotalCode), len(scores), pluralize(len(scores), "project", "projects"))
	if opts.authorExcluded > 0 {
		summaryParts += fmt.Sprintf(" (%d excluded — no commits)", opts.authorExcluded)
	}
	fmt.Fprintf(out, "  %s\n", ui.Dim(summaryParts))
	fmt.Fprintf(out, "  %s\n", ui.Dim("Since "+cfg.ProjectStart.Format("Jan 2, 2006")))
	return nil
}

func leverageOutputByAuthor(cmd *cobra.Command, ctx context.Context, agg *intleverage.LeverageScore, scores []*intleverage.LeverageScore, resolved []intleverage.ResolvedProject, resolver *intleverage.AuthorResolver, opts leverageOutputOpts) error {
	out := cmd.OutOrStdout()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	type authorAgg struct {
		displayName string
		authorID    string
		lines       int
		estPM       float64
		actualPM    float64
		leverage    float64
	}

	// Map authorID → lines owned + ownership-weighted estimated PM.
	byID := make(map[string]*authorAgg)
	var campaignLines int
	for i, proj := range resolved {
		var score *intleverage.LeverageScore
		if i < len(scores) {
			score = scores[i]
		}
		projEstPM := 0.0
		if score != nil {
			// When --by-author is used without --author, scores are full-tree.
			// Recompute from unscaled people×months.
			projEstPM = score.EstimatedPeople * score.EstimatedMonths
		}

		// Sum lines once for ownership fractions within this project.
		totalLines := 0
		for _, a := range proj.Authors {
			totalLines += a.Lines
		}

		for _, a := range proj.Authors {
			if resolver.IsExcluded(a.Email) {
				continue
			}
			authorID := resolver.Resolve(a.Email)
			entry, ok := byID[authorID]
			if !ok {
				entry = &authorAgg{
					displayName: resolver.DisplayName(authorID),
					authorID:    authorID,
				}
				byID[authorID] = entry
			}
			entry.lines += a.Lines
			campaignLines += a.Lines
			if totalLines > 0 && projEstPM > 0 {
				entry.estPM += projEstPM * (float64(a.Lines) / float64(totalLines))
			}
		}
	}

	// Personal actual PM: union of commit spans across unique git dirs (not sum).
	// Drop authors under 1% of campaign blamed lines (same threshold as effort calc).
	authors := make([]*authorAgg, 0, len(byID))
	for authorID, entry := range byID {
		if campaignLines > 0 && float64(entry.lines)/float64(campaignLines)*100 < 1.0 {
			continue
		}
		match := intleverage.AuthorMatch{
			Filter:    authorID,
			AuthorIDs: map[string]bool{authorID: true},
			Emails:    authorEmailsForID(resolver, authorID),
		}
		if len(match.Emails) == 0 {
			match.Emails = []string{authorID}
		}
		actualPM, err := intleverage.AuthorActualPersonMonths(ctx, resolved, match)
		if err != nil || actualPM <= 0 {
			actualPM = 0.1
		}
		entry.actualPM = actualPM
		if actualPM > 0 {
			entry.leverage = entry.estPM / actualPM
		}
		authors = append(authors, entry)
	}
	sort.Slice(authors, func(i, j int) bool {
		return authors[i].leverage > authors[j].leverage
	})

	headers := []string{"AUTHOR", "ID", "LINES OWNED", "EST PM", "ACTUAL PM", "LEVERAGE"}
	var rows [][]string
	for _, a := range authors {
		rows = append(rows, []string{
			a.displayName,
			a.authorID,
			fmtInt(a.lines),
			fmt.Sprintf("%.1f", a.estPM),
			fmt.Sprintf("%.1f", a.actualPM),
			fmtScore(a.leverage) + "x",
		})
	}

	label := "Campaign Leverage:"
	if opts.directoryMode {
		label = fmt.Sprintf("Directory Leverage (%s):", opts.directoryName)
	}
	fmt.Fprintf(out, "%s %s\n\n",
		ui.Header(label),
		ui.Value(fmtScore(agg.FullLeverage)+"x", ui.AccentColor))

	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(ui.DimColor)).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return headerStyle
			}
			switch col {
			case 0:
				return lipgloss.NewStyle().Foreground(ui.AccentColor)
			case 5:
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)
	fmt.Fprintln(out)
	fmt.Fprintln(out, ui.Dim("Est PM = project COCOMO person-months × author LOC share (per project, summed)"))
	fmt.Fprintln(out, ui.Dim("Actual PM = union of author's commit span across unique git repos (not summed per project)"))
	fmt.Fprintln(out, ui.Dim("Leverage = Est PM / Actual PM"))
	return nil
}

// authorEmailsForID returns configured emails for a canonical author ID.
func authorEmailsForID(resolver *intleverage.AuthorResolver, authorID string) []string {
	if resolver == nil {
		return nil
	}
	// ExpandAuthorFilter with the ID as filter reuses group matching.
	match := intleverage.ExpandAuthorFilter(resolver, authorID)
	var emails []string
	for _, e := range match.Emails {
		if e != authorID {
			emails = append(emails, e)
		}
	}
	// Prefer configured emails only; fall back to ID if nothing else.
	if len(emails) == 0 {
		return []string{authorID}
	}
	return emails
}

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

func fmtCost(f float64) string {
	return fmtInt(int(f))
}

func fmtRecentLeverage(f float64) string {
	if f < 0 {
		return fmt.Sprintf("%.1f (negative)", f)
	}
	return fmtScore(f)
}

func fmtScore(f float64) string {
	if f >= 1000 {
		return fmt.Sprintf("%s.%d", fmtInt(int(f)), int(f*10)%10)
	}
	return fmt.Sprintf("%.1f", f)
}

func pluralize(n int, singular, plural string) string {
	if n == 1 {
		return singular
	}
	return plural
}
