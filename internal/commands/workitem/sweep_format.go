package workitem

import (
	"fmt"
	"io"
	"path/filepath"

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
	// The info mark is two columns plus a space, matching the fresh "── " mark
	// so this row's status lines up with a fresh checklist above it.
	if err := ui.WriteChecklistRow(out, width, "  ", ui.InfoIcon()+"  ", title, status, ui.StatusMuted); err != nil {
		return err
	}
	fact := fmt.Sprintf("%s (%s): %s", filepath.ToSlash(path), typ, sentence)
	return ui.WriteChecklistDetail(out, width, "     ", fact)
}
