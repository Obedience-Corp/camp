package git

import (
	"regexp"
	"strings"

	"github.com/Obedience-Corp/camp/internal/slug"
)

const campaignTagMaxIDLen = 8

// legacyTagMarker is the leading token used before tags embedded the campaign
// name. It remains the fallback when no name resolves, and ParseTag still
// recognizes it so historical commits resolve.
const legacyTagMarker = "OBEY-CAMPAIGN"

// Segment markers. The emitter and the parse table (tagSegments) both read
// them from here so the two sides cannot drift apart.
const (
	tagQuestPrefix    = "qst_"
	tagFestPrefix     = "FE-"
	tagPhasePrefix    = "PH-"
	tagSequencePrefix = "SQ-"
	tagWorkitemPrefix = "WI-"
	tagNotePrefix     = "NT-"
)

// ritualFestRefPrefix is the festival-id marker fest prepends to ritual
// festivals (RI-XX0001). It is part of FestRef, not a tag segment of its own,
// so the FE- splitter must consume it instead of stopping at the first dash.
const ritualFestRefPrefix = "RI-"

// FormatTag is the canonical campaign tag emitter: every tag camp writes is
// composed here. The leading token is the slugified campaign name plus the
// short id ("[obey-campaign:8deed8b4]"), falling back to
// "[OBEY-CAMPAIGN-<id>]" when the name has no slug or the id lacks the hex
// shape the parser requires. Segments then follow in fixed order: quest,
// festival, phase, sequence, workitem, note.
//
// Phase is emitted only alongside a festival ref, and sequence only alongside
// a phase, because a phase or sequence number indexes into a festival and
// carries no meaning without it. Both are also shape-checked against the same
// regexes the parser applies, so FormatTag never emits a segment its own
// parser would reject or silently truncate.
//
// It is lenient by design and never fails: a component that cannot be emitted
// verbatim is dropped. Callers that need to know that happened should run
// ValidateTagComponents first, which reports exactly what FormatTag would
// drop. Returns "" when CampaignID is empty.
func FormatTag(tc TagComponents) string {
	if tc.CampaignID == "" {
		return ""
	}
	shortID := shortCampaignID(tc.CampaignID)

	// Only emit the name-style head when shortID has the hex shape the parser
	// requires (isNameStyleHead); otherwise fall back to the legacy form so the
	// emit and parse sides cannot diverge.
	head := legacyTagMarker + "-" + shortID
	if nameSlug := slug.Generate(tc.CampaignName); nameSlug != "" && tagNameStyleIDRe.MatchString(shortID) {
		head = nameSlug + ":" + shortID
	}

	parts := []string{head}
	if tc.QuestID != "" {
		parts = append(parts, tc.QuestID)
	}
	if tc.FestRef != "" {
		parts = append(parts, tagFestPrefix+tc.FestRef)
		// The shape checks stand in for the "is it set" checks: neither regex
		// matches the empty string, and a value that fails one would reparse
		// as a shape failure (PH-001_IMPLEMENT) or, worse, truncate silently
		// at its first dash (PH-1-2 parsing back as phase "1").
		if tagPhaseRe.MatchString(tc.Phase) {
			parts = append(parts, tagPhasePrefix+tc.Phase)
			if tagSequenceRe.MatchString(tc.Sequence) {
				parts = append(parts, tagSequencePrefix+tc.Sequence)
			}
		}
	}
	if tc.WorkitemRef != "" {
		// The ref already carries WI-, so it is self-identifying and embedded
		// verbatim (no extra marker).
		parts = append(parts, ensureTagPrefix(tc.WorkitemRef, tagWorkitemPrefix))
	}
	if tc.NoteRef != "" {
		parts = append(parts, ensureTagPrefix(tc.NoteRef, tagNotePrefix))
	}
	return "[" + strings.Join(parts, "-") + "]"
}

// shortCampaignID truncates a campaign id to the length a tag carries.
func shortCampaignID(id string) string {
	if len(id) > campaignTagMaxIDLen {
		return id[:campaignTagMaxIDLen]
	}
	return id
}

func ensureTagPrefix(ref, prefix string) string {
	if strings.HasPrefix(ref, prefix) {
		return ref
	}
	return prefix + ref
}

// FormatContextTagsFull builds the campaign tag from positional components,
// delegating to FormatTag. noteRef is optional (mirroring FormatCampaignTag's
// questID) so existing callers are unaffected; only the note commit path
// passes one, and it may co-occur with workitemRef since they describe
// different things (the ambient context a note was captured in vs. the note
// itself). Callers that need the festival phase and sequence segments build a
// TagComponents and call FormatTag directly.
// Returns "" when campaignID is empty.
func FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef string, noteRef ...string) string {
	nr := ""
	if len(noteRef) > 0 {
		nr = noteRef[0]
	}
	return FormatTag(TagComponents{
		CampaignID:   campaignID,
		CampaignName: campaignName,
		QuestID:      questID,
		FestRef:      festRef,
		WorkitemRef:  workitemRef,
		NoteRef:      nr,
	})
}

// FormatCampaignTag returns the legacy id-only "[OBEY-CAMPAIGN-{id}]" prefix,
// optionally appending a quest id. Truncates campaignID to 8 chars.
func FormatCampaignTag(campaignID string, questID ...string) string {
	qid := ""
	if len(questID) > 0 {
		qid = questID[0]
	}
	return FormatContextTagsFull("", campaignID, qid, "", "")
}

// PrependCampaignTag prepends the legacy id-only tag to a message.
func PrependCampaignTag(campaignID, message string) string {
	return PrependContextTagsFull("", campaignID, "", "", "", message)
}

// FormatContextTags returns the campaign/quest tag prefix.
func FormatContextTags(campaignName, campaignID, questID string) string {
	return FormatContextTagsFull(campaignName, campaignID, questID, "", "")
}

// PrependContextTags prepends the campaign/quest tag to a message.
func PrependContextTags(campaignName, campaignID, questID, message string) string {
	return PrependContextTagsFull(campaignName, campaignID, questID, "", "", message)
}

// PrependContextTagsFull prepends the full campaign tag to a message,
// returning it unchanged when campaignID is empty.
func PrependContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef, message string) string {
	tag := FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef)
	if tag == "" {
		return message
	}
	return tag + " " + message
}

// TagComponents are the parsed pieces of a campaign tag; empty fields were absent.
type TagComponents struct {
	CampaignID   string `json:"campaign_id"`
	CampaignName string `json:"campaign_name,omitempty"` // slug, name-style tags only
	QuestID      string `json:"quest_id"`
	FestRef      string `json:"fest_ref"`
	Phase        string `json:"phase,omitempty"`    // digits only, no PH- prefix
	Sequence     string `json:"sequence,omitempty"` // digits only, no SQ- prefix
	WorkitemRef  string `json:"workitem_ref"`       // carries the WI- prefix
	NoteRef      string `json:"note_ref"`           // carries the NT- prefix
}

// leadingTagRegex captures the leading bracket content; tags are only honored
// at position 0 (see ParseTagDetailed).
var leadingTagRegex = regexp.MustCompile(`^\[([^\]]+)\]`)

// tagBodyScanRegex matches name-style or legacy tags anywhere in a string, for
// body-grep callers only. ParseTag uses leadingTagRegex instead.
var tagBodyScanRegex = regexp.MustCompile(`\[(?:` + legacyTagMarker + `-[^\]]+|[a-z0-9][a-z0-9-]*:[0-9a-f]{1,8}[^\]]*)\]`)

var (
	tagWorkitemRefRe = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)
	tagNoteRefRe     = regexp.MustCompile(`^NT-[0-9a-f]{6}$`)
	tagQuestIDRe     = regexp.MustCompile(`^qst_[A-Za-z0-9_]{1,40}$`)
	// Ordinary festival refs are PREFIX+digits (CW0003). Ritual refs keep the
	// RI- marker as part of the id (RI-XX0001).
	tagFestRefRe  = regexp.MustCompile(`^(?:RI-)?[A-Za-z0-9]{1,32}$`)
	tagPhaseRe    = regexp.MustCompile(`^[0-9]{1,4}$`)
	tagSequenceRe = regexp.MustCompile(`^[0-9]{1,4}$`)
	// Real campaign ids are UUID-derived hex; gating on it rejects ordinary
	// bracket prefixes like "[scope:msg]".
	tagNameStyleIDRe  = regexp.MustCompile(`^[0-9a-f]{1,8}$`)
	tagCampaignNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// TagParseWarning records a degraded parse: a component that failed its shape
// check (and was zeroed) or an unknown segment.
type TagParseWarning struct {
	Field  string
	Value  string
	Reason string
}

// tagSegment describes one known segment of the grammar: how to cut it off the
// front of the remainder, the shape its payload must satisfy, and which
// TagComponents field it fills.
type tagSegment struct {
	prefix string
	field  string
	shape  *regexp.Regexp
	// want describes the shape in the terms a reader sees, for both the parse
	// warning and the ValidateTagComponents error.
	want string
	// stripPrefix drops the marker before the shape check, for segments whose
	// stored value excludes it (FE-, PH-, SQ-). Quest, workitem, and note
	// values keep their self-identifying prefix.
	stripPrefix bool
	// normalize marks the segments FormatTag will prefix for a caller that
	// supplied a bare ref, so validation checks the value that would be
	// emitted rather than the one that was passed.
	normalize bool
	// guarded marks the segments FormatTag shape-checks before emitting them
	// (see the guards there). A bad value in a guarded segment is dropped; a
	// bad value anywhere else is written into the tag as given and shows up
	// only when the tag is parsed back.
	guarded bool
	split   func(string) (string, string)
	target  func(*TagComponents) *string
}

// fate describes what FormatTag does with a value that fails this segment's
// shape check.
func (s tagSegment) fate() string {
	if s.guarded {
		return "dropped on emit"
	}
	return "emitted verbatim but reparses degraded"
}

// shapeReason is the parse warning text for a value that failed s.shape.
func (s tagSegment) shapeReason() string {
	return "shape check failed (want " + s.want + ")"
}

// tagSegments is scanned in order for a prefix match; the prefixes are
// mutually exclusive, so position within the tag does not matter.
var tagSegments = []tagSegment{
	{
		prefix: tagQuestPrefix, field: "quest_id", shape: tagQuestIDRe,
		want:   "qst_<id>",
		split:  splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.QuestID },
	},
	{
		prefix: tagFestPrefix, field: "fest_ref", shape: tagFestRefRe,
		want:        "1 to 32 letters or digits, optionally RI- prefixed",
		stripPrefix: true, split: splitFestRef,
		target: func(tc *TagComponents) *string { return &tc.FestRef },
	},
	{
		prefix: tagPhasePrefix, field: "phase", shape: tagPhaseRe,
		want:        "1 to 4 digits",
		guarded:     true,
		stripPrefix: true, split: splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.Phase },
	},
	{
		prefix: tagSequencePrefix, field: "sequence", shape: tagSequenceRe,
		want:        "1 to 4 digits",
		guarded:     true,
		stripPrefix: true, split: splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.Sequence },
	},
	{
		prefix: tagWorkitemPrefix, field: "workitem_ref", shape: tagWorkitemRefRe,
		want:      "WI-<6 hex>",
		normalize: true,
		split:     splitWorkitemSegment,
		target:    func(tc *TagComponents) *string { return &tc.WorkitemRef },
	},
	{
		prefix: tagNotePrefix, field: "note_ref", shape: tagNoteRefRe,
		want:      "NT-<6 hex>",
		normalize: true,
		split:     splitTerminalSegment,
		target:    func(tc *TagComponents) *string { return &tc.NoteRef },
	},
}

// parse consumes this segment from the front of rest, filling out and
// returning the remainder plus any warning. A shape failure or a duplicate
// leaves the field untouched, so the first valid occurrence wins.
func (s tagSegment) parse(rest string, out *TagComponents) (string, *TagParseWarning) {
	body := rest
	if s.stripPrefix {
		body = rest[len(s.prefix):]
	}
	seg, after := s.split(body)
	dst := s.target(out)
	switch {
	case !s.shape.MatchString(seg):
		return after, &TagParseWarning{Field: s.field, Value: seg, Reason: s.shapeReason()}
	case *dst != "":
		return after, &TagParseWarning{
			Field: s.field, Value: seg,
			Reason: "duplicate " + s.field + " segment",
		}
	}
	*dst = seg
	return after, nil
}

// tagSegmentFor returns the segment whose prefix leads rest.
func tagSegmentFor(rest string) (tagSegment, bool) {
	for _, s := range tagSegments {
		if strings.HasPrefix(rest, s.prefix) {
			return s, true
		}
	}
	return tagSegment{}, false
}

// ParseTag extracts the components of a leading campaign tag, returning a
// zero value when none is present.
func ParseTag(subject string) TagComponents {
	tc, _ := ParseTagDetailed(subject)
	return tc
}

// ParseTagDetailed is the warnings-aware peer of ParseTag. It accepts both the
// name-style and legacy tag forms, then peels segments by their prefixes,
// zeroing and reporting any that fail their shape check.
func ParseTagDetailed(subject string) (TagComponents, []TagParseWarning) {
	out, rest, ok := parseTagHead(subject)
	if !ok {
		return TagComponents{}, nil
	}

	var warnings []TagParseWarning

	idEnd := strings.Index(rest, "-")
	if idEnd < 0 {
		out.CampaignID = rest
		return out, warnings
	}
	out.CampaignID = rest[:idEnd]
	rest = rest[idEnd+1:]

	for rest != "" {
		segment, known := tagSegmentFor(rest)
		if !known {
			seg, after := splitAtDash(rest)
			warnings = append(warnings, TagParseWarning{
				Field: "unknown", Value: seg,
				Reason: "unknown segment between known prefixes",
			})
			rest = after
			continue
		}
		after, warning := segment.parse(rest, &out)
		if warning != nil {
			warnings = append(warnings, *warning)
		}
		rest = after
	}

	return out, append(warnings, tagDependencyWarnings(out)...)
}

// parseTagHead peels the leading bracket and its head token, returning
// components seeded with any campaign name plus the remainder left to scan.
func parseTagHead(subject string) (TagComponents, string, bool) {
	m := leadingTagRegex.FindStringSubmatch(subject)
	if m == nil {
		return TagComponents{}, "", false
	}
	inner := m[1]

	var out TagComponents
	switch {
	case isNameStyleHead(inner):
		colon := strings.IndexByte(inner, ':')
		out.CampaignName = inner[:colon]
		return out, inner[colon+1:], true
	case strings.HasPrefix(inner, legacyTagMarker+"-"):
		return out, inner[len(legacyTagMarker)+1:], true
	default:
		return TagComponents{}, "", false
	}
}

// isNameStyleHead reports whether inner leads with a "<name-slug>:<hex-id>" head.
func isNameStyleHead(inner string) bool {
	colon := strings.IndexByte(inner, ':')
	if colon <= 0 {
		return false
	}
	if !tagCampaignNameRe.MatchString(inner[:colon]) {
		return false
	}
	id, _ := splitAtDash(inner[colon+1:])
	return tagNameStyleIDRe.MatchString(id)
}

// splitAtDash splits s at the first "-", returning (s, "") when none is present.
func splitAtDash(s string) (string, string) {
	if i := strings.Index(s, "-"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

// splitFestRef extracts the festival ref from an FE- payload. Ritual ids
// (RI-XX0001) contain an internal dash that is not a segment boundary;
// ordinary ids (CW0003) have none, so they still split at the first dash.
func splitFestRef(payload string) (string, string) {
	if strings.HasPrefix(payload, ritualFestRefPrefix) {
		token, after := splitAtDash(payload[len(ritualFestRefPrefix):])
		return ritualFestRefPrefix + token, after
	}
	return splitAtDash(payload)
}

// splitTerminalSegment consumes the whole remainder as one segment. NT- is
// last in the fixed order, so anything trailing it belongs to the same
// (malformed) value rather than to a separate segment.
func splitTerminalSegment(s string) (string, string) {
	return s, ""
}

// splitWorkitemSegment extracts a WI- segment from the front of rest,
// returning the remainder that follows it. splitAtDash cannot be used here:
// the historical doubled form (WI-WI-abcdef) has an internal dash that is
// not a segment boundary. On a shape match (single or doubled prefix plus
// exactly 6 lowercase hex chars) it returns the normalized single-prefix
// ref and the trailing remainder (leading "-" stripped) so a following
// segment such as NT- is left for the next parse iteration. On a shape
// mismatch it falls back to the legacy behavior of treating the entire
// remainder as the malformed value (doubled-prefix stripped once, mirroring
// the historical ref-detection step) so existing malformed-tag warnings are
// unchanged.
func splitWorkitemSegment(rest string) (seg, after string) {
	doubled := strings.HasPrefix(rest, tagWorkitemPrefix+tagWorkitemPrefix)
	prefixLen := len(tagWorkitemPrefix)
	if doubled {
		prefixLen = 2 * len(tagWorkitemPrefix)
	}
	if len(rest) >= prefixLen+6 && isLowerHex(rest[prefixLen:prefixLen+6]) {
		end := prefixLen + 6
		return tagWorkitemPrefix + rest[prefixLen:end], strings.TrimPrefix(rest[end:], "-")
	}
	seg = rest
	if doubled {
		seg = rest[len(tagWorkitemPrefix):]
	}
	return seg, ""
}

// isLowerHex reports whether s consists solely of lowercase hex digits.
func isLowerHex(s string) bool {
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
