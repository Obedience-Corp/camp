package intent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
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

// GetNote retrieves a note by its frontmatter id from the note store.
// Resolution is id-authoritative, not filename-based, so a renamed note whose
// slug has drifted from its id still resolves.
func (s *IntentService) GetNote(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	path, err := s.resolveNoteByID(id)
	if err != nil {
		return nil, err
	}
	return s.loadIntent(path)
}

// resolveNoteByID returns the path of the note whose id: frontmatter equals id.
// Notes are few and live only under NoteStatuses(), so a filename fast path
// followed by a small directory scan (covering renamed notes) is enough — no
// cached index is needed here, unlike the lifecycle id index.
func (s *IntentService) resolveNoteByID(id string) (string, error) {
	// Fast path: filename == id (an unrenamed note).
	for _, status := range NoteStatuses() {
		p := s.getIntentPath(status, id)
		if note, err := s.loadIntent(p); err == nil && note.ID == id {
			return p, nil
		}
	}
	// Slow path: a renamed note whose filename no longer matches its id.
	for _, status := range NoteStatuses() {
		dir := filepath.Join(s.intentsDir, string(status))
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
				continue
			}
			p := filepath.Join(dir, f.Name())
			if note, err := s.loadIntent(p); err == nil && note.ID == id {
				return p, nil
			}
		}
	}
	return "", camperrors.Wrap(ErrNotFound, id)
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

// UpdateNoteTags is the note-store equivalent of UpdateDirect's tag path: notes
// resolve outside the lifecycle id index UpdateDirect uses, so they need their
// own entry point that still normalizes/validates tags, refreshes updated_at,
// and returns a FieldChange (empty when unchanged) for the caller's audit/commit.
func (s *IntentService) UpdateNoteTags(ctx context.Context, id string, tags []string) (*Intent, []audit.FieldChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, camperrors.Wrap(err, "context cancelled")
	}

	normTags, err := validateAndNormalizeTags(tags)
	if err != nil {
		return nil, nil, err
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, nil, err
	}

	if slices.Equal(note.Tags, normTags) {
		return note, nil, nil
	}

	change := audit.FieldChange{
		Field: "tags",
		Old:   strings.Join(note.Tags, ","),
		New:   strings.Join(normTags, ","),
	}
	note.Tags = normTags
	note.UpdatedAt = time.Now()

	if err := s.Save(ctx, note); err != nil {
		return nil, nil, err
	}
	return note, []audit.FieldChange{change}, nil
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
	if _, statErr := os.Stat(newPath); statErr == nil {
		return nil, camperrors.Wrap(ErrFileExists, newPath)
	} else if !os.IsNotExist(statErr) {
		return nil, camperrors.Wrap(statErr, "checking destination note file")
	}
	if err := fsutil.WriteFileAtomically(newPath, data, 0644); err != nil {
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

// RestoreNote moves a note back from notes/archived/ into the active notes/
// store, reversing ArchiveNote so an archived note is never a dead end.
func (s *IntentService) RestoreNote(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}

	if note.Status == StatusNote {
		return note, nil
	}

	oldPath := note.Path
	note.Status = StatusNote
	note.UpdatedAt = time.Now()

	newPath := s.getIntentPath(StatusNote, note.ID)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	data, err := SerializeIntent(note)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing note")
	}
	if _, statErr := os.Stat(newPath); statErr == nil {
		return nil, camperrors.Wrap(ErrFileExists, newPath)
	} else if !os.IsNotExist(statErr) {
		return nil, camperrors.Wrap(statErr, "checking destination note file")
	}
	if err := fsutil.WriteFileAtomically(newPath, data, 0644); err != nil {
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

// MoveIntentToNote moves a lifecycle intent into the active notes/ store. The
// file keeps its title, body, author, tags, and timestamps, but drops lifecycle
// metadata that notes do not carry.
func (s *IntentService) MoveIntentToNote(ctx context.Context, id string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	if _, err := s.GetNote(ctx, id); err == nil {
		return nil, camperrors.Wrap(ErrFileExists, id)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	// Resolve by exact id, never a fuzzy substring match: this relocates a file
	// and strips its lifecycle metadata, so acting on a truncated or mistyped id
	// would silently convert the wrong intent.
	it, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if it.Status.IsNote() {
		return it, nil
	}

	oldPath := it.Path
	it.Status = StatusNote
	it.Type = ""
	it.Concept = ""
	it.Priority = ""
	it.Horizon = ""
	it.BlockedBy = nil
	it.DependsOn = nil
	it.PromotionCriteria = ""
	it.PromotedTo = ""
	it.GatheredFrom = nil
	it.GatheredAt = time.Time{}
	it.GatheredInto = ""
	it.UpdatedAt = time.Now()

	newPath := s.moveTargetPath(it.ID, StatusNote, oldPath)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	data, err := SerializeIntent(it)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing note")
	}
	if err := fsutil.WriteFileAtomically(newPath, data, 0644); err != nil {
		return nil, camperrors.Wrap(err, "writing note file")
	}
	if err := os.Remove(oldPath); err != nil {
		_ = os.Remove(newPath)
		return nil, camperrors.Wrap(err, "removing old intent file")
	}

	s.removeAllCopies(id, newPath)
	it.Path = newPath
	s.invalidateIDIndex()
	return it, nil
}

// Convert promotes a note into inbox, attaches the given type, and refreshes
// updated_at. Use MoveNoteToStatus when the caller already has a target status.
func (s *IntentService) Convert(ctx context.Context, id string, newType Type) (*Intent, error) {
	return s.MoveNoteToStatus(ctx, id, StatusInbox, newType)
}

// MoveNoteToStatus promotes a note into the selected non-note lifecycle status.
func (s *IntentService) MoveNoteToStatus(ctx context.Context, id string, newStatus Status, newType Type) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	if newStatus.IsNote() || !isLifecycleStatus(newStatus) {
		return nil, camperrors.Wrapf(ErrInvalidStatus, "%q", newStatus)
	}
	if newType != "" && !isValidType(newType) {
		return nil, camperrors.Wrapf(ErrInvalidType, "%q", newType)
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}
	if _, err := s.resolveByID(note.ID); err == nil {
		return nil, camperrors.Wrap(ErrFileExists, note.ID)
	} else if !errors.Is(err, ErrNotFound) {
		return nil, err
	}

	oldPath := note.Path
	note.Status = newStatus
	note.Type = newType
	note.UpdatedAt = time.Now()

	newPath := s.moveTargetPath(note.ID, newStatus, oldPath)
	if err := os.MkdirAll(filepath.Dir(newPath), 0755); err != nil {
		return nil, camperrors.Wrap(err, "creating directory")
	}

	data, err := SerializeIntent(note)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing converted note")
	}
	if err := fsutil.WriteFileAtomically(newPath, data, 0644); err != nil {
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

func isLifecycleStatus(status Status) bool {
	return slices.Contains(AllStatuses(), status)
}

// noteSortKey returns the timestamp used to order notes (updated, else created).
func noteSortKey(n *Intent) time.Time {
	if !n.UpdatedAt.IsZero() {
		return n.UpdatedAt
	}
	return n.CreatedAt
}
