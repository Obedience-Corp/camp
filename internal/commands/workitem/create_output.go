package workitem

import (
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/ui"
)

const createTrackingOnlyHint = "Tracking only: directory and .workitem marker, no scaffold."

// createSummary is the human-readable result of camp workitem create.
type createSummary struct {
	Type         string
	TypeExplicit bool
	TypeFrom     string
	Slug         string
	Path         string
	ID           string
	Ref          string
	QuestID      string
	Next         string
	Hint         string
}

func writeCreateSummary(w io.Writer, s createSummary) error {
	lines := []string{
		fmt.Sprintf("%s Created %s workitem %s", ui.SuccessIcon(), s.Type, s.Slug),
		createSummaryRow("path:", ui.Value(s.Path)),
		createSummaryRow("id:", ui.Value(s.ID)),
		createSummaryRow("ref:", ui.Value(s.Ref)),
		createSummaryRow("type:", ui.Value(s.Type)+createTypeOrigin(s)),
	}
	if s.QuestID != "" {
		lines = append(lines, createSummaryRow("quest:", ui.Value(s.QuestID)))
	}
	if s.Next != "" {
		lines = append(lines, createSummaryRow("next:", ui.Value(s.Next)))
	}
	if s.Hint != "" {
		lines = append(lines, ui.Dim("  "+s.Hint))
	}
	for _, line := range lines {
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func createSummaryRow(label, value string) string {
	return "  " + ui.Label(fmt.Sprintf("%-6s", label)) + " " + value
}

func createTypeOrigin(s createSummary) string {
	switch {
	case s.TypeFrom != "":
		return " " + ui.Dim("(from "+s.TypeFrom+")")
	case !s.TypeExplicit:
		return " " + ui.Dim("(default)")
	default:
		return ""
	}
}
