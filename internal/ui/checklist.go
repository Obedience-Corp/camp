package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ChecklistLabelWidth is the label column shared by aligned CLI checklists.
// A status starts at the same column on every row whose label fits. A longer
// label keeps a single space before its status instead of being truncated.
const ChecklistLabelWidth = 32

// StatusTone is how a checklist status is colored. The text itself stays plain
// so a row can wrap on word boundaries before the color is applied.
type StatusTone int

const (
	// StatusPlain leaves the status in the default foreground.
	StatusPlain StatusTone = iota
	// StatusMuted is a quiet result: already done, nothing to do, still running.
	StatusMuted
	// StatusSuccess is a result that changed something or finished cleanly.
	StatusSuccess
	// StatusWarning is a result the operator should notice and can continue past.
	StatusWarning
	// StatusError is a result that stopped the step.
	StatusError
)

// TermColumns reports the stdout terminal width. A pipe, a redirected
// descriptor, or an unknown size returns 0, and checklist rows then stay on
// one line so a transcript remains one record per step.
func TermColumns() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 0
	}
	return w
}

// WriteChecklistRow writes one aligned checklist row.
//
// indent and mark are literal prefixes (mark includes its trailing space).
// status is plain text. When width is positive and the status does not fit in
// the remaining columns, later pieces move to the next line at the status
// column and break on whitespace. A single word that is wider than the column
// stays intact.
func WriteChecklistRow(w io.Writer, width int, indent, mark, label, status string, tone StatusTone) error {
	if status == "" {
		_, err := fmt.Fprintf(w, "%s%s%s\n", indent, mark, label)
		return err
	}

	gap := ChecklistLabelWidth - lipgloss.Width(label)
	if gap < 1 {
		gap = 1
	}
	statusCol := lipgloss.Width(indent) + lipgloss.Width(mark) + lipgloss.Width(label) + gap
	avail := 0
	if width > statusCol {
		avail = width - statusCol
	}
	parts := wrapFields(status, avail)
	if len(parts) == 0 {
		parts = []string{status}
	}

	if _, err := fmt.Fprintf(w, "%s%s%s%s%s\n", indent, mark, label, strings.Repeat(" ", gap), styleTone(parts[0], tone)); err != nil {
		return err
	}
	cont := strings.Repeat(" ", statusCol)
	for _, part := range parts[1:] {
		if _, err := fmt.Fprintf(w, "%s%s\n", cont, styleTone(part, tone)); err != nil {
			return err
		}
	}
	return nil
}

// WriteChecklistDetail writes text under a checklist row. Lines break on
// whitespace within the space left of width. A width of 0 keeps the text on
// one line.
func WriteChecklistDetail(w io.Writer, width int, indent, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	avail := 0
	if width > lipgloss.Width(indent) {
		avail = width - lipgloss.Width(indent)
	}
	for _, line := range wrapFields(text, avail) {
		if _, err := fmt.Fprintf(w, "%s%s\n", indent, line); err != nil {
			return err
		}
	}
	return nil
}

func styleTone(text string, tone StatusTone) string {
	switch tone {
	case StatusMuted:
		return Dim(text)
	case StatusSuccess:
		return Success(text)
	case StatusWarning:
		return Warning(text)
	case StatusError:
		return Error(text)
	default:
		return text
	}
}

// wrapFields splits s on whitespace into lines of at most width visual columns.
// width <= 0 disables wrapping. A field wider than width occupies its own line.
func wrapFields(s string, width int) []string {
	if s == "" {
		return nil
	}
	if width <= 0 || lipgloss.Width(s) <= width {
		return []string{s}
	}
	fields := strings.Fields(s)
	var (
		lines []string
		b     strings.Builder
		lineW int
	)
	for _, field := range fields {
		fw := lipgloss.Width(field)
		if lineW == 0 {
			b.WriteString(field)
			lineW = fw
			continue
		}
		if lineW+1+fw > width {
			lines = append(lines, b.String())
			b.Reset()
			b.WriteString(field)
			lineW = fw
			continue
		}
		b.WriteByte(' ')
		b.WriteString(field)
		lineW += 1 + fw
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())
	}
	return lines
}
