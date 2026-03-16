package leverage

import (
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

func leverageOutputByAuthor(cmd *cobra.Command, agg *intleverage.LeverageScore, resolved []intleverage.ResolvedProject, resolver *intleverage.AuthorResolver, opts leverageOutputOpts) error {
	out := cmd.OutOrStdout()
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(ui.CategoryColor)

	type authorAgg struct {
		displayName string
		authorID    string
		lines       int
		weightedPM  float64
	}

	byID := make(map[string]*authorAgg)
	for _, proj := range resolved {
		for _, a := range proj.Authors {
			authorID := resolver.Resolve(a.Email)
			if existing, ok := byID[authorID]; ok {
				existing.lines += a.Lines
				existing.weightedPM += a.WeightedPM
				continue
			}
			byID[authorID] = &authorAgg{
				displayName: resolver.DisplayName(authorID),
				authorID:    authorID,
				lines:       a.Lines,
				weightedPM:  a.WeightedPM,
			}
		}
	}

	authors := make([]*authorAgg, 0, len(byID))
	for _, a := range byID {
		authors = append(authors, a)
	}
	sort.Slice(authors, func(i, j int) bool {
		return authors[i].weightedPM > authors[j].weightedPM
	})

	headers := []string{"AUTHOR", "ID", "LINES OWNED", "WEIGHTED PM", "LEVERAGE SHARE"}
	var rows [][]string
	for _, a := range authors {
		levShare := 0.0
		if agg.ActualPersonMonths > 0 {
			levShare = (a.weightedPM / agg.ActualPersonMonths) * agg.FullLeverage
		}
		rows = append(rows, []string{
			a.displayName,
			a.authorID,
			fmtInt(a.lines),
			fmt.Sprintf("%.2f", a.weightedPM),
			fmtScore(levShare) + "x",
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
			case 4:
				return lipgloss.NewStyle().Foreground(ui.SuccessColor)
			default:
				return lipgloss.NewStyle()
			}
		})

	fmt.Fprintln(out, t)
	fmt.Fprintln(out)
	fmt.Fprintln(out, ui.Dim("Weighted PM = author's date span × (author's LOC / total LOC)"))
	fmt.Fprintln(out, ui.Dim("Leverage Share = (author's weighted PM / total actual PM) × campaign leverage"))
	return nil
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
