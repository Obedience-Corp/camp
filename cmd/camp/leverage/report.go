package leverage

import (
	"fmt"
	"strings"

	intleverage "github.com/Obedience-Corp/camp/internal/leverage"
)

var scoreTableHeaders = []string{
	"PROJECT", "FILES", "CODE", "AUTHORS", "EST COST", "EST PM", "ACTUAL PM", "LEVERAGE",
}

// renderScoreReport renders the leverage summary as plain text for a commit
// body. It carries the same numbers as the terminal table and none of its
// styling: a commit message is read by git, diff tools, and forges that would
// show terminal escapes literally.
func renderScoreReport(
	agg *intleverage.LeverageScore,
	scores []*intleverage.LeverageScore,
	cfg *intleverage.LeverageConfig,
	opts leverageOutputOpts,
) string {
	var b strings.Builder
	b.WriteString(scoreHeadline(agg, opts))
	b.WriteString("\n\n")
	b.WriteString(renderPlainTable(scoreTableHeaders, buildScoreRows(scores)))
	b.WriteString("\n")
	for _, line := range scoreTotals(agg, scores, cfg, opts) {
		b.WriteString(line)
		b.WriteString("\n")
	}
	return strings.TrimRight(b.String(), "\n")
}

func scoreHeadline(agg *intleverage.LeverageScore, opts leverageOutputOpts) string {
	if opts.authorFilter != "" {
		return fmt.Sprintf("Your Leverage: %sx (%s)", fmtScore(agg.FullLeverage), opts.authorFilter)
	}
	headline := fmt.Sprintf("Campaign Leverage: %sx", fmtScore(agg.FullLeverage))
	if agg.AuthorCount > 0 {
		headline += fmt.Sprintf(" (%d %s detected)",
			agg.AuthorCount, pluralize(agg.AuthorCount, "author", "authors"))
	}
	return headline
}

func scoreTotals(
	agg *intleverage.LeverageScore,
	scores []*intleverage.LeverageScore,
	cfg *intleverage.LeverageConfig,
	opts leverageOutputOpts,
) []string {
	actualPM := agg.ActualPersonMonths
	if actualPM == 0 {
		actualPM = agg.ActualPeople * agg.ElapsedMonths
	}
	effortLabel := "Actual Effort"
	if opts.authorFilter != "" {
		effortLabel = "Your Effort"
	}

	summary := fmt.Sprintf("%s lines of code across %d %s",
		fmtInt(agg.TotalCode), len(scores), pluralize(len(scores), "project", "projects"))
	if opts.authorExcluded > 0 {
		summary += fmt.Sprintf(" (%d excluded, no commits)", opts.authorExcluded)
	}

	lines := []string{
		fmt.Sprintf("COCOMO Estimate: %s person-months ($%s)",
			fmtInt(int(agg.EstimatedPeople*agg.EstimatedMonths)), fmtCost(agg.EstimatedCost)),
		fmt.Sprintf("%s: %.1f person-months", effortLabel, actualPM),
		fmt.Sprintf("Team Equivalent: %sx", fmtScore(agg.SimpleLeverage)),
		"",
		summary,
	}
	if !cfg.ProjectStart.IsZero() {
		lines = append(lines, "Since "+cfg.ProjectStart.Format("Jan 2, 2006"))
	}
	return lines
}

// renderPlainTable lays rows out under headers with a two-space gutter.
// Every cell is left-aligned so the width calculation stays byte-exact for the
// ASCII the score formatters produce.
func renderPlainTable(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}

	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}

	var b strings.Builder
	writeRow(&b, widths, headers)
	for _, row := range rows {
		writeRow(&b, widths, row)
	}
	return b.String()
}

func writeRow(b *strings.Builder, widths []int, cells []string) {
	parts := make([]string, 0, len(cells))
	for i, cell := range cells {
		if i < len(widths) && i < len(cells)-1 {
			cell += strings.Repeat(" ", widths[i]-len(cell))
		}
		parts = append(parts, cell)
	}
	b.WriteString(strings.TrimRight(strings.Join(parts, "  "), " "))
	b.WriteString("\n")
}
