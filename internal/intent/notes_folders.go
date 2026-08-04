package intent

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// noteFolderSegmentPattern is lowercase kebab-case: starts/ends alphanumeric,
// interior may include single hyphens (matching tag-style normalization).
var noteFolderSegmentPattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

// ErrNoteFolderNotEmpty is returned when DeleteNoteFolder is asked to remove a
// folder that still holds notes or child directories.
var ErrNoteFolderNotEmpty = errors.New("note folder is not empty")

// ErrNoteFolderExists is returned when CreateNoteFolder would collide with an
// existing directory.
var ErrNoteFolderExists = errors.New("note folder already exists")

// NoteFolder describes one directory under the notes store.
// Status is the relative directory under .campaign/intents/ (e.g. "notes",
// "notes/reading/papers"). Depth 0 is the notes root.
type NoteFolder struct {
	Status   Status
	Name     string
	Depth    int
	Reserved bool
	Count    int // notes directly in this folder (.md files only)
	ModTime  time.Time
}

// reservedNoteFolderNames are fixed first-level folders under notes/.
// Order matches the explorer sketch: meetings first, then archived.
var reservedNoteFolderNames = []string{"meetings", "archived"}

// NoteFolders walks .campaign/intents/notes/ and returns every note folder.
// Order: notes root first, reserved folders next (even if missing on disk),
// then user folders alphabetically depth-first. Dot-prefixed directories
// (.transcripts/) are skipped.
func (s *IntentService) NoteFolders(ctx context.Context) ([]NoteFolder, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	rootDir, err := s.statusDir(StatusNote)
	if err != nil {
		return nil, err
	}

	root := NoteFolder{
		Status:   StatusNote,
		Name:     "Notes",
		Depth:    0,
		Reserved: false,
	}
	fillFolderStats(&root, rootDir)

	out := []NoteFolder{root}

	// Collect first-level directory names that exist (user folders only for
	// the alpha walk; reserved are always injected next).
	userNames := make([]string, 0)
	if entries, readErr := os.ReadDir(rootDir); readErr == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			if isReservedNoteFolderName(name) {
				continue
			}
			userNames = append(userNames, name)
		}
	}
	sort.Strings(userNames)

	// Reserved folders always appear, even when not yet created on disk.
	for _, name := range reservedNoteFolderNames {
		status := Status(string(StatusNote) + "/" + name)
		folder := NoteFolder{
			Status:   status,
			Name:     noteFolderDisplayName(name, true),
			Depth:    1,
			Reserved: true,
		}
		dir, dirErr := s.statusDir(status)
		if dirErr == nil {
			fillFolderStats(&folder, dir)
		}
		out = append(out, folder)
	}

	// User folders, depth-first alphabetical.
	for _, name := range userNames {
		status := Status(string(StatusNote) + "/" + name)
		dir, dirErr := s.statusDir(status)
		if dirErr != nil {
			continue
		}
		walked, walkErr := s.walkUserNoteFolders(ctx, dir, status, 1)
		if walkErr != nil {
			return nil, walkErr
		}
		out = append(out, walked...)
	}

	return out, nil
}

// walkUserNoteFolders returns the folder at dir/status and its descendants,
// depth-first with sibling directories sorted alphabetically.
func (s *IntentService) walkUserNoteFolders(ctx context.Context, dir string, status Status, depth int) ([]NoteFolder, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	folder := NoteFolder{
		Status:   status,
		Name:     noteFolderDisplayName(filepath.Base(string(status)), false),
		Depth:    depth,
		Reserved: false,
	}
	fillFolderStats(&folder, dir)
	out := []NoteFolder{folder}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, camperrors.Wrapf(err, "reading note folder %s", dir)
	}

	childNames := make([]string, 0)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		childNames = append(childNames, name)
	}
	sort.Strings(childNames)

	for _, name := range childNames {
		childStatus := Status(string(status) + "/" + name)
		childDir := filepath.Join(dir, name)
		walked, walkErr := s.walkUserNoteFolders(ctx, childDir, childStatus, depth+1)
		if walkErr != nil {
			return nil, walkErr
		}
		out = append(out, walked...)
	}
	return out, nil
}

// noteFolderStatuses returns every discovered note folder Status for scan
// paths (resolve / list). Missing reserved folders still appear so callers
// can attempt them without a separate list.
func (s *IntentService) noteFolderStatuses(ctx context.Context) ([]Status, error) {
	folders, err := s.NoteFolders(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(folders))
	for _, f := range folders {
		out = append(out, f.Status)
	}
	return out, nil
}

func fillFolderStats(folder *NoteFolder, dir string) {
	info, err := os.Stat(dir)
	if err != nil {
		return
	}
	folder.ModTime = info.ModTime()

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".md") {
			count++
		}
	}
	folder.Count = count
}

func isReservedNoteFolderName(name string) bool {
	for _, r := range reservedNoteFolderNames {
		if name == r {
			return true
		}
	}
	return false
}

// noteFolderDisplayName returns the UI label for a folder segment.
// Reserved folders are title-cased; user folders keep the on-disk name with
// the first rune uppercased for top-level readability (e.g. "reading" → "Reading").
func noteFolderDisplayName(segment string, reserved bool) string {
	if segment == "" {
		return segment
	}
	if reserved {
		switch segment {
		case "archived":
			return "Archived"
		case "meetings":
			return "Meetings"
		}
	}
	r, size := utf8.DecodeRuneInString(segment)
	if r == utf8.RuneError {
		return segment
	}
	return string(unicode.ToUpper(r)) + segment[size:]
}

// isArchivedNoteStatus reports whether status is the archived bucket or nested under it.
func isArchivedNoteStatus(status Status) bool {
	if status == StatusNoteArchived {
		return true
	}
	return strings.HasPrefix(string(status), string(StatusNoteArchived)+"/")
}

// noteStatusForFolder maps a user-supplied relative folder path (under notes/)
// to a Status. Empty folder means the notes root. The path is normalized and
// validated (kebab-case segments, no reserved root names, no traversal/dots).
func noteStatusForFolder(folder string) (Status, error) {
	rel, err := normalizeNoteFolderRel(folder)
	if err != nil {
		return "", err
	}
	if rel == "" {
		return StatusNote, nil
	}
	return Status(string(StatusNote) + "/" + rel), nil
}

// normalizeNoteFolderRel cleans and validates a folder path relative to notes/.
// Returns "" for the notes root. Rejects absolute paths, traversal, reserved
// root names, dot-prefixed segments, and non-kebab-case segments.
func normalizeNoteFolderRel(folder string) (string, error) {
	raw := strings.TrimSpace(folder)
	if raw == "" || raw == "." {
		return "", nil
	}
	// Reject absolute paths and Windows-style drives early.
	if filepath.IsAbs(raw) || strings.HasPrefix(raw, "/") || strings.Contains(raw, ":") {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder path must be relative: %q", folder)
	}

	// Normalize separators and clean.
	cleaned := filepath.ToSlash(filepath.Clean(raw))
	if cleaned == "." {
		return "", nil
	}
	if strings.HasPrefix(cleaned, "../") || cleaned == ".." || strings.Contains(cleaned, "/../") {
		return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder path must not contain '..': %q", folder)
	}
	// Strip a leading "notes/" if the caller passed a full status path.
	cleaned = strings.TrimPrefix(cleaned, string(StatusNote)+"/")
	if cleaned == string(StatusNote) {
		return "", nil
	}

	parts := strings.Split(cleaned, "/")
	for i, part := range parts {
		if part == "" || part == "." {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder path has empty segment: %q", folder)
		}
		if part == ".." {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder path must not contain '..': %q", folder)
		}
		if strings.HasPrefix(part, ".") {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder segment must not be dot-prefixed: %q", part)
		}
		// Normalize to lowercase kebab-case.
		norm := strings.ToLower(strings.TrimSpace(part))
		norm = strings.ReplaceAll(norm, "_", "-")
		norm = strings.ReplaceAll(norm, " ", "-")
		if !noteFolderSegmentPattern.MatchString(norm) {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput,
				"note folder segment %q must be lowercase kebab-case (a-z, 0-9, hyphens)", part)
		}
		// Reserved names only forbidden at the notes root (depth 1).
		if i == 0 && isReservedNoteFolderName(norm) {
			return "", camperrors.Wrapf(camperrors.ErrInvalidInput,
				"note folder name %q is reserved", norm)
		}
		parts[i] = norm
	}
	return strings.Join(parts, "/"), nil
}

// CreateNoteFolder creates a note folder (and parents) under notes/, writing a
// .gitkeep so empty folders survive git. Rel is relative to notes/ (e.g.
// "reading/papers"). Reserved names, traversal, and collisions are rejected.
func (s *IntentService) CreateNoteFolder(ctx context.Context, rel string) (NoteFolder, error) {
	if err := ctx.Err(); err != nil {
		return NoteFolder{}, camperrors.Wrap(err, "context cancelled")
	}

	status, err := noteStatusForFolder(rel)
	if err != nil {
		return NoteFolder{}, err
	}
	if status == StatusNote {
		return NoteFolder{}, camperrors.Wrap(camperrors.ErrInvalidInput, "cannot create the notes root")
	}

	dir, err := s.statusDir(status)
	if err != nil {
		return NoteFolder{}, err
	}
	if info, statErr := os.Stat(dir); statErr == nil && info.IsDir() {
		return NoteFolder{}, camperrors.Wrap(ErrNoteFolderExists, string(status))
	} else if statErr != nil && !os.IsNotExist(statErr) {
		return NoteFolder{}, camperrors.Wrap(statErr, "checking note folder")
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return NoteFolder{}, camperrors.Wrap(err, "creating note folder")
	}
	gitkeep := filepath.Join(dir, ".gitkeep")
	if _, statErr := os.Stat(gitkeep); os.IsNotExist(statErr) {
		if writeErr := os.WriteFile(gitkeep, []byte{}, 0o644); writeErr != nil {
			return NoteFolder{}, camperrors.Wrap(writeErr, "writing .gitkeep")
		}
	}

	folder := NoteFolder{
		Status:   status,
		Name:     noteFolderDisplayName(filepath.Base(string(status)), false),
		Depth:    strings.Count(string(status), "/"),
		Reserved: false,
	}
	fillFolderStats(&folder, dir)
	return folder, nil
}

// RenameNoteFolder renames a note folder via os.Rename. Contained notes keep
// their files; Status is derived from location on the next load, so frontmatter
// is not rewritten. From and to are relative to notes/.
func (s *IntentService) RenameNoteFolder(ctx context.Context, from, to string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	fromStatus, err := noteStatusForFolder(from)
	if err != nil {
		return err
	}
	toStatus, err := noteStatusForFolder(to)
	if err != nil {
		return err
	}
	if fromStatus == StatusNote || toStatus == StatusNote {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "cannot rename the notes root")
	}
	if isReservedNoteStatus(fromStatus) || isReservedNoteStatus(toStatus) {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "cannot rename reserved note folders")
	}
	if fromStatus == toStatus {
		return nil
	}

	fromDir, err := s.statusDir(fromStatus)
	if err != nil {
		return err
	}
	toDir, err := s.statusDir(toStatus)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(fromDir); statErr != nil {
		return camperrors.Wrapf(statErr, "source note folder %s", fromStatus)
	}
	if _, statErr := os.Stat(toDir); statErr == nil {
		return camperrors.Wrap(ErrNoteFolderExists, string(toStatus))
	} else if !os.IsNotExist(statErr) {
		return camperrors.Wrap(statErr, "checking destination note folder")
	}

	if err := os.MkdirAll(filepath.Dir(toDir), 0o755); err != nil {
		return camperrors.Wrap(err, "creating parent of destination folder")
	}
	if err := os.Rename(fromDir, toDir); err != nil {
		return camperrors.Wrapf(err, "renaming note folder %s -> %s", fromStatus, toStatus)
	}
	s.invalidateIDIndex()
	return nil
}

// DeleteNoteFolder removes an empty note folder. It refuses when the folder
// still contains notes (.md) or child directories (other than .gitkeep / dots).
func (s *IntentService) DeleteNoteFolder(ctx context.Context, rel string) error {
	if err := ctx.Err(); err != nil {
		return camperrors.Wrap(err, "context cancelled")
	}

	status, err := noteStatusForFolder(rel)
	if err != nil {
		return err
	}
	if status == StatusNote {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "cannot delete the notes root")
	}
	if isReservedNoteStatus(status) {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "cannot delete reserved note folders")
	}

	dir, err := s.statusDir(status)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return camperrors.Wrapf(err, "reading note folder %s", status)
	}
	for _, e := range entries {
		name := e.Name()
		if name == ".gitkeep" {
			continue
		}
		if strings.HasPrefix(name, ".") && !e.IsDir() {
			// Allow other dotfiles? Prefer refuse for anything but .gitkeep.
			if name != ".gitkeep" {
				return camperrors.Wrapf(ErrNoteFolderNotEmpty, "%s contains %s", status, name)
			}
			continue
		}
		return camperrors.Wrapf(ErrNoteFolderNotEmpty, "%s contains %s", status, name)
	}

	// Remove .gitkeep then the directory.
	_ = os.Remove(filepath.Join(dir, ".gitkeep"))
	if err := os.Remove(dir); err != nil {
		return camperrors.Wrapf(err, "removing note folder %s", status)
	}
	return nil
}

// MoveNoteToFolder moves a note into the given folder (relative to notes/;
// empty = root). This is intentionally separate from MoveNoteToStatus, which
// promotes a note into a lifecycle intent status.
func (s *IntentService) MoveNoteToFolder(ctx context.Context, id, folder string) (*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}

	newStatus, err := noteStatusForFolder(folder)
	if err != nil {
		return nil, err
	}

	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}
	if note.Status == newStatus {
		return note, nil
	}

	// Destination folder must exist for non-root targets (reserved/user).
	if newStatus != StatusNote {
		dir, dirErr := s.statusDir(newStatus)
		if dirErr != nil {
			return nil, dirErr
		}
		if _, statErr := os.Stat(dir); statErr != nil {
			if os.IsNotExist(statErr) {
				return nil, camperrors.Wrapf(camperrors.ErrNotFound, "note folder %s", newStatus)
			}
			return nil, camperrors.Wrap(statErr, "checking destination note folder")
		}
	}

	oldPath := note.Path
	note.Status = newStatus
	note.UpdatedAt = time.Now()

	newPath, err := s.moveTargetPath(note.ID, newStatus, oldPath)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o755); err != nil {
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
	if err := fsutil.WriteFileAtomically(newPath, data, 0o644); err != nil {
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

func isReservedNoteStatus(status Status) bool {
	return status == StatusNoteArchived || status == StatusNoteMeetings
}

// requireNoteFolderExists returns an error when status is a non-root note
// folder that is not present on disk. Used by CreateNote --folder (must exist).
func (s *IntentService) requireNoteFolderExists(status Status) error {
	if status == StatusNote {
		return nil
	}
	dir, err := s.statusDir(status)
	if err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return camperrors.Wrapf(camperrors.ErrNotFound, "note folder %s", status)
		}
		return camperrors.Wrap(err, "checking note folder")
	}
	if !info.IsDir() {
		return camperrors.Wrapf(camperrors.ErrInvalidInput, "note folder path is not a directory: %s", status)
	}
	return nil
}
