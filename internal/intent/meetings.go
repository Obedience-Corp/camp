package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
)

// ImportMeetingOptions configures meeting note import from a voice-agent bundle
// directory (or any directory containing a summary + transcript).
type ImportMeetingOptions struct {
	// BundlePath is the absolute path to the meeting bundle directory.
	BundlePath string
	// Summary is optional body text. When empty, SummaryFile or a summary.md
	// inside the bundle is used.
	Summary string
	// SummaryFile is an optional path to a summary markdown file.
	SummaryFile string
	// Title overrides the derived title.
	Title string
	// TranscriptFile overrides the default transcript.md / transcript path.
	TranscriptFile string
	// AdoptIntentID, when set, deletes that lifecycle intent after a successful
	// import (migration of misfiled meeting intents).
	AdoptIntentID string
	// Author attribution for the note.
	Author string
	// Timestamp for id generation when creating a new note.
	Timestamp time.Time
}

// ImportMeetingResult is returned by ImportMeeting.
type ImportMeetingResult struct {
	Note            *Intent
	TranscriptPath  string
	UpdatedExisting bool
}

// ImportMeeting creates or updates a note under notes/meetings/ with a
// transcript sidecar in notes/meetings/.transcripts/. Re-import of the same
// bundle id updates in place rather than duplicating.
func (s *IntentService) ImportMeeting(ctx context.Context, opts ImportMeetingOptions) (*ImportMeetingResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	if strings.TrimSpace(opts.BundlePath) == "" {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "bundle path is required")
	}

	bundle := filepath.Clean(opts.BundlePath)
	info, err := os.Stat(bundle)
	if err != nil {
		return nil, camperrors.Wrapf(err, "stat bundle %s", bundle)
	}
	if !info.IsDir() {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "bundle path must be a directory: %s", bundle)
	}

	base := strings.TrimSuffix(filepath.Base(bundle), ".meeting")
	if base == "" {
		base = filepath.Base(bundle)
	}

	summary, err := resolveMeetingSummary(opts)
	if err != nil {
		return nil, err
	}
	transcript, transcriptSrc, err := resolveMeetingTranscript(opts, bundle)
	if err != nil {
		return nil, err
	}

	title := opts.Title
	if title == "" {
		title = "Meeting: " + strings.ReplaceAll(base, "-", " ")
	}

	// Ensure meetings folder exists.
	if _, err := s.CreateNoteFolder(ctx, "meetings"); err != nil && !isNoteFolderExists(err) {
		// CreateNoteFolder rejects reserved names — meetings is reserved.
		// Create the directory directly.
		meetingsDir, dirErr := s.statusDir(StatusNoteMeetings)
		if dirErr != nil {
			return nil, dirErr
		}
		if mkErr := os.MkdirAll(meetingsDir, 0o755); mkErr != nil {
			return nil, camperrors.Wrap(mkErr, "creating notes/meetings")
		}
	}
	meetingsDir, err := s.statusDir(StatusNoteMeetings)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(meetingsDir, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "creating notes/meetings")
	}
	transcriptsDir := filepath.Join(meetingsDir, ".transcripts")
	if err := os.MkdirAll(transcriptsDir, 0o755); err != nil {
		return nil, camperrors.Wrap(err, "creating .transcripts")
	}

	// Stable id from bundle basename when it already looks like an intent id;
	// otherwise generate via CreateNote then rewrite.
	id := base
	if !intentIDPattern.MatchString(id) {
		// slug-YYYYMMDD-HHMMSS if possible; else generate.
		ts := opts.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		data := NewTemplateDataFromInput(title, "", "", opts.Author, summary, ts)
		id = data.ID
	}

	// Re-import: if a note with this id already exists under meetings, update it.
	existingPath := filepath.Join(meetingsDir, id+".md")
	updatedExisting := false
	var note *Intent
	if _, statErr := os.Stat(existingPath); statErr == nil {
		updatedExisting = true
		note, err = s.loadIntent(existingPath)
		if err != nil {
			return nil, err
		}
		note.Title = title
		note.Content = summary
		note.Status = StatusNoteMeetings
		note.UpdatedAt = time.Now()
		if opts.Author != "" {
			note.Author = opts.Author
		}
	} else {
		// Create via template then place in meetings.
		ts := opts.Timestamp
		if ts.IsZero() {
			ts = time.Now()
		}
		// Force id by writing file directly with desired id.
		data := NewTemplateDataFromInput(title, "", "", opts.Author, summary, ts)
		// Override generated id when base is a valid id.
		if intentIDPattern.MatchString(base) {
			// Reconstruct template with fixed id — use CreateNote into meetings
			// via manual write.
		}
		_ = data
		note = &Intent{
			ID:        id,
			Title:     title,
			Status:    StatusNoteMeetings,
			Author:    opts.Author,
			Content:   summary,
			CreatedAt: ts,
		}
		if note.CreatedAt.IsZero() {
			note.CreatedAt = time.Now()
		}
	}

	// Body: summary with transcript pointer.
	transcriptRel := ".transcripts/" + id + ".md"
	body := strings.TrimSpace(summary)
	if body == "" {
		body = "# " + title + "\n\n## Summary\n\n_(no summary provided)_\n"
	}
	if !strings.Contains(body, transcriptRel) {
		body = body + "\n\n## Transcript\n\nFull transcript: `" + transcriptRel + "`\n"
	}
	note.Content = body
	note.Status = StatusNoteMeetings
	note.Path = existingPath

	data, err := SerializeIntent(note)
	if err != nil {
		return nil, camperrors.Wrap(err, "serializing meeting note")
	}
	if err := fsutil.WriteFileAtomically(existingPath, data, 0o644); err != nil {
		return nil, camperrors.Wrap(err, "writing meeting note")
	}
	note.Path = existingPath

	// Write transcript sidecar.
	transcriptPath := filepath.Join(transcriptsDir, id+".md")
	if transcript == "" && transcriptSrc != "" {
		raw, readErr := os.ReadFile(transcriptSrc)
		if readErr != nil {
			return nil, camperrors.Wrap(readErr, "reading transcript")
		}
		transcript = string(raw)
	}
	if err := fsutil.WriteFileAtomically(transcriptPath, []byte(transcript), 0o644); err != nil {
		return nil, camperrors.Wrap(err, "writing transcript sidecar")
	}

	// Optional adopt: remove misfiled lifecycle intent.
	if opts.AdoptIntentID != "" {
		if delErr := s.Delete(ctx, opts.AdoptIntentID); delErr != nil {
			// Non-fatal: import succeeded; surface adopt failure in result path.
			return &ImportMeetingResult{
				Note:            note,
				TranscriptPath:  transcriptPath,
				UpdatedExisting: updatedExisting,
			}, camperrors.Wrap(delErr, "import succeeded but adopt-intent delete failed")
		}
	}

	s.invalidateIDIndex()
	return &ImportMeetingResult{
		Note:            note,
		TranscriptPath:  transcriptPath,
		UpdatedExisting: updatedExisting,
	}, nil
}

// ExtractItem is one action-item/intent extracted from a meeting note.
type ExtractItem struct {
	Title string
	Body  string
}

// ExtractMeetingIntents creates inbox intents from extract items and records
// their ids on the meeting note. Re-running with the same titles is idempotent:
// existing extracted ids whose titles still match are kept; duplicates are not
// recreated.
func (s *IntentService) ExtractMeetingIntents(ctx context.Context, id string, items []ExtractItem) ([]*Intent, error) {
	if err := ctx.Err(); err != nil {
		return nil, camperrors.Wrap(err, "context cancelled")
	}
	note, err := s.GetNote(ctx, id)
	if err != nil {
		return nil, err
	}
	if note.Status != StatusNoteMeetings {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "note %s is not a meeting (status %s)", id, note.Status)
	}

	// Collect already-linked ids from tags or content marker lines.
	// Minimal contract: store extracted ids in tags as "from-meeting:<id>" on intents
	// and list them in the meeting body under a machine-readable section.
	existing := parseExtractedIntentIDs(note.Content)
	existingSet := make(map[string]bool, len(existing))
	for _, e := range existing {
		existingSet[e] = true
	}

	// Title set of already-created extracts for idempotency.
	existingTitles := make(map[string]bool)
	for eid := range existingSet {
		if it, gerr := s.Get(ctx, eid); gerr == nil {
			existingTitles[strings.ToLower(it.Title)] = true
		}
	}

	var created []*Intent
	for _, item := range items {
		title := strings.TrimSpace(item.Title)
		if title == "" {
			continue
		}
		if existingTitles[strings.ToLower(title)] {
			continue
		}
		body := strings.TrimSpace(item.Body)
		if body == "" {
			body = "Extracted from meeting " + id
		}
		body = body + "\n\nmeeting_ref: " + id + "\n"
		it, cerr := s.CreateDirect(ctx, CreateOptions{
			Title: title,
			Type:  TypeIdea,
			Body:  body,
			Tags:  []string{"from-meeting"},
		})
		if cerr != nil {
			return created, cerr
		}
		created = append(created, it)
		existing = append(existing, it.ID)
		existingTitles[strings.ToLower(title)] = true
	}

	// Rewrite meeting body extracted section.
	note.Content = upsertExtractedSection(note.Content, existing)
	note.UpdatedAt = time.Now()
	if err := s.Save(ctx, note); err != nil {
		return created, err
	}
	return created, nil
}

func parseExtractedIntentIDs(content string) []string {
	const marker = "extracted_intents:"
	for _, line := range strings.Split(content, "\n") {
		trim := strings.TrimSpace(line)
		if !strings.HasPrefix(trim, marker) {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(trim, marker))
		rest = strings.Trim(rest, "[]")
		if rest == "" {
			return nil
		}
		parts := strings.Split(rest, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			p = strings.Trim(p, `"'`)
			if p != "" {
				out = append(out, p)
			}
		}
		return out
	}
	return nil
}

func upsertExtractedSection(content string, ids []string) string {
	line := "extracted_intents: [" + strings.Join(ids, ", ") + "]"
	lines := strings.Split(content, "\n")
	found := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "extracted_intents:") {
			lines[i] = line
			found = true
			break
		}
	}
	if !found {
		if !strings.HasSuffix(content, "\n") && content != "" {
			content += "\n"
		}
		return content + "\n" + line + "\n"
	}
	return strings.Join(lines, "\n")
}

// MeetingTranscriptPath returns the sidecar path for a meeting note when present.
func (s *IntentService) MeetingTranscriptPath(note *Intent) (string, bool) {
	if note == nil || !strings.HasPrefix(string(note.Status), string(StatusNoteMeetings)) {
		return "", false
	}
	dir, err := s.statusDir(StatusNoteMeetings)
	if err != nil {
		return "", false
	}
	p := filepath.Join(dir, ".transcripts", note.ID+".md")
	if _, err := os.Stat(p); err != nil {
		return "", false
	}
	return p, true
}

func resolveMeetingSummary(opts ImportMeetingOptions) (string, error) {
	if opts.Summary != "" {
		return opts.Summary, nil
	}
	if opts.SummaryFile != "" {
		raw, err := os.ReadFile(opts.SummaryFile)
		if err != nil {
			return "", camperrors.Wrap(err, "reading summary file")
		}
		return string(raw), nil
	}
	// Try common names inside the bundle.
	for _, name := range []string{"summary.md", "note.md", "README.md"} {
		p := filepath.Join(opts.BundlePath, name)
		if raw, err := os.ReadFile(p); err == nil {
			return string(raw), nil
		}
	}
	return "", nil
}

func resolveMeetingTranscript(opts ImportMeetingOptions, bundle string) (content, srcPath string, err error) {
	if opts.TranscriptFile != "" {
		raw, readErr := os.ReadFile(opts.TranscriptFile)
		if readErr != nil {
			return "", "", camperrors.Wrap(readErr, "reading transcript file")
		}
		return string(raw), opts.TranscriptFile, nil
	}
	for _, name := range []string{"transcript.md", "transcript.txt", "full_transcript.md"} {
		p := filepath.Join(bundle, name)
		if raw, readErr := os.ReadFile(p); readErr == nil {
			return string(raw), p, nil
		}
	}
	return "", "", nil
}

func isNoteFolderExists(err error) bool {
	return err != nil && (err == ErrNoteFolderExists || strings.Contains(err.Error(), "already exists"))
}
