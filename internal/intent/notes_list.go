package intent

import (
	"context"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// NoteEntry is a note paired with the folder that holds it on disk.
type NoteEntry struct {
	Note *Intent
	// Folder is the holding directory relative to notes/, "" for the notes root.
	Folder string
}

// ListNoteEntries returns the ListNotes result with each note's folder. Folder
// and Note.Status come from the file location rather than frontmatter, because
// RenameNoteFolder moves files without rewriting their status field.
func (s *IntentService) ListNoteEntries(ctx context.Context, includeArchived bool) ([]NoteEntry, error) {
	notes, err := s.ListNotes(ctx, includeArchived)
	if err != nil {
		return nil, err
	}
	notesRoot, err := s.statusDir(StatusNote)
	if err != nil {
		return nil, err
	}

	entries := make([]NoteEntry, 0, len(notes))
	for _, note := range notes {
		folder, err := noteFolderFromPath(notesRoot, note.Path)
		if err != nil {
			return nil, camperrors.Wrapf(err, "locating note %s", note.ID)
		}
		note.Status = noteFolderStatus(folder)
		entries = append(entries, NoteEntry{Note: note, Folder: folder})
	}
	return entries, nil
}

// ResolveNoteFolder canonicalizes a folder filter relative to notes/ and
// confirms the folder is part of the note store. Unlike NormalizeNoteFolderRel
// it accepts the reserved meetings and archived folders, since a filter only
// reads. "", ".", and "notes" name the notes root.
func (s *IntentService) ResolveNoteFolder(ctx context.Context, folder string) (string, error) {
	rel := filepath.ToSlash(filepath.Clean(strings.TrimSpace(folder)))
	rel = strings.TrimPrefix(rel, string(StatusNote)+"/")
	if rel == "." || rel == string(StatusNote) {
		return "", nil
	}

	folders, err := s.NoteFolders(ctx)
	if err != nil {
		return "", err
	}
	want := noteFolderStatus(rel)
	for _, f := range folders {
		if f.Status == want {
			return rel, nil
		}
	}
	return "", camperrors.NewNotFound("note folder", folder, nil)
}

func noteFolderFromPath(notesRoot, notePath string) (string, error) {
	rel, err := filepath.Rel(notesRoot, filepath.Dir(notePath))
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note %s is outside %s", notePath, notesRoot)
	}
	if rel == "." {
		return "", nil
	}
	return rel, nil
}

func noteFolderStatus(folder string) Status {
	if folder == "" {
		return StatusNote
	}
	return Status(string(StatusNote) + "/" + folder)
}
