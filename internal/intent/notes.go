package intent

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// CreateNote creates a note in the flat notes/ store, bypassing the
// inbox/ready/active intent lifecycle. Notes carry no type and are not shown by
// normal intent listings; tags organize them.
func (s *IntentService) CreateNote(ctx context.Context, opts CreateOptions) (*Intent, error) {
	opts.Category = CategoryNote
	opts.Type = ""
	opts.Concept = ""
	return s.CreateDirect(ctx, opts)
}

// GetNote retrieves a note by its exact ID from the note store.
func (s *IntentService) GetNote(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	for _, status := range NoteStatuses() {
		path := s.getIntentPath(status, id)
		if note, err := s.loadIntent(path); err == nil {
			return note, nil
		}
	}

	return nil, camperrors.Wrap(ErrNotFound, id)
}

// ListNotes returns notes from the note store, newest first. By default only
// the active notes/ directory is listed; pass includeArchived to also include
// notes/archived/.
func (s *IntentService) ListNotes(ctx context.Context, includeArchived bool) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	statuses := []Status{StatusNote}
	if includeArchived {
		statuses = NoteStatuses()
	}

	var notes []*Intent
	for _, status := range statuses {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, camperrors.Wrapf(err, "reading directory %s", dir)
		}

		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
				continue
			}
			note, err := s.loadIntent(filepath.Join(dir, file.Name()))
			if err != nil {
				continue
			}
			notes = append(notes, note)
		}
	}

	sort.Slice(notes, func(i, j int) bool {
		return noteSortKey(notes[i]).After(noteSortKey(notes[j]))
	})

	return notes, nil
}

// ArchiveNote moves a note from notes/ into notes/archived/.
func (s *IntentService) ArchiveNote(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}

	if note.Status == StatusNoteArchived {
		return note, nil
	}

	oldPath := note.Path
	note.Status = StatusNoteArchived
	note.UpdatedAt = time.Now()

	newPath := s.getIntentPath(StatusNoteArchived, note.ID)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	data, err := SerializeIntent(note)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing note")
	}
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing note file")
	}

	if err := os.Remove(oldPath); err != nil {
		_ = os.Remove(newPath)
		return nil, camperrors.Wrap(err, "removing old note file")
	}

	note.Path = newPath
	s.invalidateIDIndex()
	return note, nil
}

// Convert promotes a note into the intent lifecycle: it moves
// notes/<id>.md into inbox/<id>.md, attaches the given type, and refreshes
// updated_at. This is the only bridge from a note into inbox/ready/active.
func (s *IntentService) Convert(ctx context.Context, id string, newType Type) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	if newType != "" && !isValidType(newType) {
		return nil, camperrors.Wrapf(ErrInvalidType, "%q", newType)
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}

	oldPath := note.Path
	note.Status = StatusInbox
	note.Type = newType
	note.UpdatedAt = time.Now()

	newPath := s.getIntentPath(StatusInbox, note.ID)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	data, err := SerializeIntent(note)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing converted note")
	}
	if err := os.WriteFile(newPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing intent file")
	}

	if err := os.Remove(oldPath); err != nil {
		_ = os.Remove(newPath)
		return nil, camperrors.Wrap(err, "removing old note file")
	}

	note.Path = newPath
	s.invalidateIDIndex()
	return note, nil
}

// noteSortKey returns the timestamp used to order notes (updated, else created).
func noteSortKey(n *Intent) time.Time {
	if !n.UpdatedAt.IsZero() {
		return n.UpdatedAt
	}
	return n.CreatedAt
}
