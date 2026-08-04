package intent

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
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
	// Timestamp sets CreatedAt when creating a new note.
	Timestamp time.Time
}

// ImportMeetingResult is returned by ImportMeeting.
type ImportMeetingResult struct {
	Note            *Intent
	TranscriptPath  string
	UpdatedExisting bool
}

// ImportMeeting creates or updates a note under notes/meetings/ with a
// transcript sidecar in notes/meetings/.transcripts/. The normalized bundle
// basename deterministically identifies the note, so re-import updates in
// place even when the basename is not already a valid intent ID.
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
	meetingPath := filepath.Join(bundle, "meeting.md")
	if _, err := os.Stat(meetingPath); err != nil {
		return nil, camperrors.Wrapf(camperrors.ErrInvalidInput, "meeting bundle is missing meeting.md: %s", meetingPath)
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

	// meetings is reserved, so create its backing directory directly instead
	// of routing through the user-folder API that intentionally rejects it.
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

	id := stableMeetingID(base)
	var adopted *Intent
	if opts.AdoptIntentID != "" {
		adopted, err = s.Get(ctx, opts.AdoptIntentID)
		if err != nil {
			return nil, camperrors.Wrap(err, "resolving intent to adopt")
		}
		id = adopted.ID
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
		ts := opts.Timestamp
		if adopted != nil && !adopted.CreatedAt.IsZero() {
			ts = adopted.CreatedAt
		}
		if ts.IsZero() {
			ts = time.Now()
		}
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
	meeting := buildMeetingMetadata(bundle, transcript, opts.Timestamp)
	meeting.Transcript = transcriptRel
	if note.Meeting != nil && len(note.Meeting.ExtractedIntents) > 0 {
		meeting.ExtractedIntents = append([]string(nil), note.Meeting.ExtractedIntents...)
	}
	note.Meeting = meeting
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

// stableMeetingID preserves already-valid bundle IDs and otherwise derives a
// deterministic valid ID from the normalized basename. The hash selects a
// synthetic timestamp solely to satisfy the repository-wide ID format; the
// note's CreatedAt remains the actual import timestamp.
func stableMeetingID(base string) string {
	if intentIDPattern.MatchString(base) {
		return base
	}
	slug := SlugFromTitle(base)
	if slug == "" {
		slug = "meeting"
	}
	normalizedBase := strings.ToLower(strings.TrimSpace(base))
	sum := sha256.Sum256([]byte(normalizedBase))
	const stableTimestampSpanSeconds = uint64(100 * 365 * 24 * 60 * 60)
	seconds := binary.BigEndian.Uint64(sum[:8]) % stableTimestampSpanSeconds
	timestamp := time.Date(2000, time.January, 1, 0, 0, 0, 0, time.UTC).
		Add(time.Duration(seconds) * time.Second)
	return GenerateID(slug, timestamp)
}

// ExtractItem is one action-item/intent extracted from a meeting note.
type ExtractItem struct {
	Title string
	Body  string
	Type  Type
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

	// Prefer the structured meeting metadata, while retaining the old body marker
	// as a one-way migration path for notes written by the initial prototype.
	existing := make([]string, 0)
	if note.Meeting != nil {
		existing = append(existing, note.Meeting.ExtractedIntents...)
	}
	if len(existing) == 0 {
		existing = parseExtractedIntentIDs(note.Content)
	}
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
		typ := item.Type
		if typ == "" {
			typ = TypeChore
		}
		it, cerr := s.CreateDirect(ctx, CreateOptions{
			Title: title,
			Type:  typ,
			Body:  body,
			Tags:  []string{"from-meeting"},
		})
		if cerr != nil {
			return created, cerr
		}
		created = append(created, it)
		existing = append(existing, it.ID)
		existingTitles[strings.ToLower(title)] = true
		if note.Meeting == nil {
			note.Meeting = &MeetingMetadata{}
		}
		note.Meeting.ExtractedIntents = append([]string(nil), existing...)
		note.UpdatedAt = time.Now()
		// Persist each backlink before creating the next intent. If a later
		// create fails, retrying extraction will not duplicate earlier work.
		if err := s.Save(ctx, note); err != nil {
			return created, err
		}
	}

	if note.Meeting == nil {
		note.Meeting = &MeetingMetadata{}
	}
	note.Meeting.ExtractedIntents = existing
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

// MeetingTranscriptPath returns the sidecar path for a meeting note when present.
func (s *IntentService) MeetingTranscriptPath(note *Intent) (string, bool) {
	if note == nil || note.Status != StatusNoteMeetings {
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
	for _, name := range []string{"meeting.md", "transcript.md", "transcript.txt", "full_transcript.md"} {
		p := filepath.Join(bundle, name)
		if raw, readErr := os.ReadFile(p); readErr == nil {
			return string(raw), p, nil
		}
	}
	return "", "", nil
}

var (
	meetingTimestampLine = regexp.MustCompile(`^\[(\d{2}):(\d{2}):(\d{2})\](?:\s*([^:]+):)?`)
	meetingSpeakerLine   = regexp.MustCompile(`^\s*([^:]{1,80}):\s+`)
)

func buildMeetingMetadata(bundle, transcript string, fallbackStart time.Time) *MeetingMetadata {
	absBundle, err := filepath.Abs(bundle)
	if err != nil {
		absBundle = bundle
	}
	host, _ := os.Hostname()
	metadata := &MeetingMetadata{
		StartedAt:        fallbackStart,
		Bundle:           absBundle,
		BundleHost:       host,
		ExtractedIntents: make([]string, 0),
	}
	if _, err := os.Stat(filepath.Join(bundle, "audio.wav")); err == nil {
		metadata.Audio = "audio.wav"
	}
	eventSpeakers, speakerAnalysis := readMeetingEventMetadata(filepath.Join(bundle, ".samantha", "events.jsonl"))
	metadata.SpeakerAnalysis = speakerAnalysis
	if metadata.SpeakerAnalysis == "" && len(eventSpeakers) > 0 {
		metadata.SpeakerAnalysis = "complete"
	}

	speakers := eventSpeakers
	lastOffset := 0
	for _, line := range strings.Split(transcript, "\n") {
		trimmed := strings.TrimSpace(line)
		header := strings.TrimSpace(strings.TrimPrefix(trimmed, "#"))
		lower := strings.ToLower(header)
		switch {
		case strings.HasPrefix(lower, "started:"):
			if started, ok := parseMeetingTime(strings.TrimSpace(header[len("started:"):])); ok {
				metadata.StartedAt = started
			}
			continue
		case strings.HasPrefix(lower, "ended:"):
			if ended, ok := parseMeetingTime(strings.TrimSpace(header[len("ended:"):])); ok {
				metadata.EndedAt = ended
			}
			continue
		case strings.HasPrefix(lower, "stt:"):
			metadata.STT = strings.TrimSpace(header[len("stt:"):])
			continue
		}
		if matches := meetingTimestampLine.FindStringSubmatch(trimmed); len(matches) == 5 {
			hours, _ := strconv.Atoi(matches[1])
			minutes, _ := strconv.Atoi(matches[2])
			seconds, _ := strconv.Atoi(matches[3])
			lastOffset = hours*3600 + minutes*60 + seconds
			metadata.Utterances++
			if speaker := strings.TrimSpace(matches[4]); speaker != "" {
				speakers[speaker] = struct{}{}
			}
			continue
		}
		if matches := meetingSpeakerLine.FindStringSubmatch(trimmed); len(matches) == 2 && isSpeakerLabel(matches[1]) {
			metadata.Utterances++
			speakers[strings.TrimSpace(matches[1])] = struct{}{}
		}
	}
	metadata.Speakers = len(speakers)
	if !metadata.StartedAt.IsZero() && !metadata.EndedAt.IsZero() && metadata.EndedAt.After(metadata.StartedAt) {
		metadata.DurationSeconds = int(metadata.EndedAt.Sub(metadata.StartedAt).Seconds())
	} else {
		metadata.DurationSeconds = lastOffset
		if !metadata.StartedAt.IsZero() && lastOffset > 0 {
			metadata.EndedAt = metadata.StartedAt.Add(time.Duration(lastOffset) * time.Second)
		}
	}
	return metadata
}

func readMeetingEventMetadata(path string) (map[string]struct{}, string) {
	speakers := make(map[string]struct{})
	raw, err := os.ReadFile(path)
	if err != nil {
		return speakers, ""
	}
	analysis := ""
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var event map[string]any
		if json.Unmarshal([]byte(line), &event) != nil {
			continue
		}
		collectMeetingEventMetadata(event, speakers, &analysis)
	}
	return speakers, analysis
}

func collectMeetingEventMetadata(value any, speakers map[string]struct{}, analysis *string) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lowerKey := strings.ToLower(key)
			if text, ok := child.(string); ok {
				switch lowerKey {
				case "speaker", "speaker_id", "speaker_label":
					if strings.TrimSpace(text) != "" {
						speakers[strings.TrimSpace(text)] = struct{}{}
					}
				case "speaker_analysis":
					*analysis = strings.TrimSpace(text)
				case "status":
					if strings.Contains(strings.ToLower(stringValue(typed["type"])), "speaker") {
						*analysis = strings.TrimSpace(text)
					}
				}
			}
			collectMeetingEventMetadata(child, speakers, analysis)
		}
	case []any:
		for _, child := range typed {
			collectMeetingEventMetadata(child, speakers, analysis)
		}
	}
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func isSpeakerLabel(value string) bool {
	label := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(label, "spk") || strings.HasPrefix(label, "speaker")
}

func parseMeetingTime(value string) (time.Time, bool) {
	for _, layout := range []string{time.RFC3339, time.RFC3339Nano} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}
