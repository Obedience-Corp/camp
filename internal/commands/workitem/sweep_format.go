package workitem

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/ui"
)

// writeSweepFact prints one sweep decision as a checklist row plus a wrapped
// sentence. The row carries the directory name and a short status. The
// sentence keeps the path, the type, and the full reason, which is what a
// transcript reader uses to see why camp left something alone.
func writeSweepFact(out io.Writer, path, typ, status, sentence string) error {
	title := filepath.Base(filepath.ToSlash(path))
	if title == "" || title == "." || title == "/" {
		title = path
	}
	width := ui.TermColumns()
	tone := sweepTone(status)
	if err := ui.WriteChecklistRow(out, width, ui.ChecklistRowIndent, ui.ChecklistMark(tone), title, status, tone); err != nil {
		return err
	}
	fact := fmt.Sprintf("%s (%s): %s", filepath.ToSlash(path), typ, sentence)
	return ui.WriteChecklistDetail(out, width, ui.ChecklistDetailIndent, fact)
}

func sweepTone(status string) ui.StatusTone {
	switch {
	case strings.HasPrefix(status, "would "):
		return ui.StatusInfo
	case status == "not moved" || strings.HasPrefix(status, "not moved"):
		return ui.StatusWarning
	default:
		return ui.StatusMuted
	}
}

func writeSweepHeading(out io.Writer) error {
	return ui.WriteChecklistSection(out, ui.TermColumns(), "Work items", "")
}
