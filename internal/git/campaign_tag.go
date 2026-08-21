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

// FormatTag is the canonical campaign tag emitter: every tag camp writes is
// composed here. The leading token is the slugified campaign name plus the
// short id ("[obey-campaign:8deed8b4]"), falling back to
// "[OBEY-CAMPAIGN-<id>]" when the name has no slug or the id lacks the hex
// shape the parser requires. Segments then follow in fixed order: quest,
// festival, phase, sequence, workitem, note.
//
// Phase is emitted only alongside a festival ref, and sequence only alongside
// a phase, because a phase or sequence number indexes into a festival and
// carries no meaning without it. Returns "" when CampaignID is empty.
func FormatTag(tc TagComponents) string {
	if tc.CampaignID == "" {
		return ""
	}
	shortID := tc.CampaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}

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
		parts = append(parts, "FE-"+tc.FestRef)
		if tc.Phase != "" {
			parts = append(parts, "PH-"+tc.Phase)
			if tc.Sequence != "" {
				parts = append(parts, "SQ-"+tc.Sequence)
			}
		}
	}
	if tc.WorkitemRef != "" {
		// The ref already carries WI-, so it is self-identifying and embedded
		// verbatim (no extra marker).
		parts = append(parts, ensureTagPrefix(tc.WorkitemRef, "WI-"))
	}
	if tc.NoteRef != "" {
		parts = append(parts, ensureTagPrefix(tc.NoteRef, "NT-"))
	}
	return "[" + strings.Join(parts, "-") + "]"
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
	tagFestRefRe     = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	tagPhaseRe       = regexp.MustCompile(`^[0-9]{1,4}$`)
	tagSequenceRe    = regexp.MustCompile(`^[0-9]{1,4}$`)
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
	prefix      string
	field       string
	shape       *regexp.Regexp
	shapeReason string
	// stripPrefix drops the marker before the shape check, for segments whose
	// stored value excludes it (FE-, PH-, SQ-). Quest, workitem, and note
	// values keep their self-identifying prefix.
	stripPrefix bool
	split       func(string) (string, string)
	target      func(*TagComponents) *string
}

// tagSegments is scanned in order for a prefix match; the prefixes are
// mutually exclusive, so position within the tag does not matter.
var tagSegments = []tagSegment{
	{
		prefix: "qst_", field: "quest_id", shape: tagQuestIDRe,
		shapeReason: "shape check failed (want qst_<id>)",
		split:       splitAtDash,
		target:      func(tc *TagComponents) *string { return &tc.QuestID },
	},
	{
		prefix: "FE-", field: "fest_ref", shape: tagFestRefRe,
		shapeReason: "shape check failed (want <PREFIX><4 digits>)",
		stripPrefix: true, split: splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.FestRef },
	},
	{
		prefix: "PH-", field: "phase", shape: tagPhaseRe,
		shapeReason: "shape check failed (want PH-<1-4 digits>)",
		stripPrefix: true, split: splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.Phase },
	},
	{
		prefix: "SQ-", field: "sequence", shape: tagSequenceRe,
		shapeReason: "shape check failed (want SQ-<1-4 digits>)",
		stripPrefix: true, split: splitAtDash,
		target: func(tc *TagComponents) *string { return &tc.Sequence },
	},
	{
		prefix: "WI-", field: "workitem_ref", shape: tagWorkitemRefRe,
		shapeReason: "shape check failed (want WI-<6 hex>)",
		split:       splitWorkitemSegment,
		target:      func(tc *TagComponents) *string { return &tc.WorkitemRef },
	},
	{
		prefix: "NT-", field: "note_ref", shape: tagNoteRefRe,
		shapeReason: "shape check failed (want NT-<6 hex>)",
		split:       splitTerminalSegment,
		target:      func(tc *TagComponents) *string { return &tc.NoteRef },
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
		return after, &TagParseWarning{Field: s.field, Value: seg, Reason: s.shapeReason}
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

	// A sequence indexes into a phase, so one without the other is reported as
	// degraded. The value is kept: it is still the best available locator.
	if out.Sequence != "" && out.Phase == "" {
		warnings = append(warnings, TagParseWarning{
			Field: "sequence", Value: out.Sequence,
			Reason: "sequence segment without phase",
		})
	}
	return out, warnings
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
	doubled := strings.HasPrefix(rest, "WI-WI-")
	prefixLen := len("WI-")
	if doubled {
		prefixLen = len("WI-WI-")
	}
	if len(rest) >= prefixLen+6 && isLowerHex(rest[prefixLen:prefixLen+6]) {
		end := prefixLen + 6
		return "WI-" + rest[prefixLen:end], strings.TrimPrefix(rest[end:], "-")
	}
	seg = rest
	if doubled {
		seg = rest[len("WI-"):]
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
