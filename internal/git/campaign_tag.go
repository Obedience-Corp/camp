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

// FormatContextTagsFull builds the campaign tag. The leading token is the
// slugified campaign name plus the short id ("[obey-campaign:8deed8b4]"),
// falling back to "[OBEY-CAMPAIGN-<id>]" when campaignName has no slug. The
// remaining components follow in fixed order: quest, festival, workitem,
// note. noteRef is optional (mirroring FormatCampaignTag's questID) so
// existing callers are unaffected; only the note commit path passes one, and
// it may co-occur with workitemRef since they describe different things (the
// ambient context a note was captured in vs. the note itself).
// Returns "" when campaignID is empty.
func FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef string, noteRef ...string) string {
	if campaignID == "" {
		return ""
	}
	shortID := campaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}

	// Only emit the name-style head when shortID has the hex shape the parser
	// requires (isNameStyleHead); otherwise fall back to the legacy form so the
	// emit and parse sides cannot diverge.
	head := legacyTagMarker + "-" + shortID
	if nameSlug := slug.Generate(campaignName); nameSlug != "" && tagNameStyleIDRe.MatchString(shortID) {
		head = nameSlug + ":" + shortID
	}

	parts := []string{head}
	if questID != "" {
		parts = append(parts, questID)
	}
	if festRef != "" {
		parts = append(parts, "FE-"+festRef)
	}
	if workitemRef != "" {
		if !strings.HasPrefix(workitemRef, "WI-") {
			workitemRef = "WI-" + workitemRef
		}
		// The ref already carries WI-, so it is self-identifying and embedded
		// verbatim (no extra marker).
		parts = append(parts, workitemRef)
	}
	nr := ""
	if len(noteRef) > 0 {
		nr = noteRef[0]
	}
	if nr != "" {
		if !strings.HasPrefix(nr, "NT-") {
			nr = "NT-" + nr
		}
		parts = append(parts, nr)
	}
	return "[" + strings.Join(parts, "-") + "]"
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
	WorkitemRef  string `json:"workitem_ref"` // carries the WI- prefix
	NoteRef      string `json:"note_ref"`     // carries the NT- prefix
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

// ParseTag extracts the components of a leading campaign tag, returning a
// zero value when none is present.
func ParseTag(subject string) TagComponents {
	tc, _ := ParseTagDetailed(subject)
	return tc
}

// ParseTagDetailed is the warnings-aware peer of ParseTag. It accepts both the
// name-style and legacy tag forms, then peels quest/festival/workitem segments
// by their prefixes, zeroing and reporting any that fail their shape check.
func ParseTagDetailed(subject string) (TagComponents, []TagParseWarning) {
	m := leadingTagRegex.FindStringSubmatch(subject)
	if m == nil {
		return TagComponents{}, nil
	}
	inner := m[1]

	var out TagComponents
	switch {
	case isNameStyleHead(inner):
		colon := strings.IndexByte(inner, ':')
		out.CampaignName = inner[:colon]
		inner = inner[colon+1:]
	case strings.HasPrefix(inner, legacyTagMarker+"-"):
		inner = inner[len(legacyTagMarker)+1:]
	default:
		return TagComponents{}, nil
	}

	var warnings []TagParseWarning

	idEnd := strings.Index(inner, "-")
	if idEnd < 0 {
		out.CampaignID = inner
		return out, warnings
	}
	out.CampaignID = inner[:idEnd]
	rest := inner[idEnd+1:]

	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "qst_"):
			seg, after := splitAtDash(rest)
			if !tagQuestIDRe.MatchString(seg) {
				warnings = append(warnings, TagParseWarning{
					Field: "quest_id", Value: seg,
					Reason: "shape check failed (want qst_<id>)",
				})
			} else if out.QuestID != "" {
				warnings = append(warnings, TagParseWarning{
					Field: "quest_id", Value: seg,
					Reason: "duplicate quest_id segment",
				})
			} else {
				out.QuestID = seg
			}
			rest = after
		case strings.HasPrefix(rest, "FE-"):
			payload := rest[len("FE-"):]
			seg, after := splitAtDash(payload)
			if !tagFestRefRe.MatchString(seg) {
				warnings = append(warnings, TagParseWarning{
					Field: "fest_ref", Value: seg,
					Reason: "shape check failed (want <PREFIX><4 digits>)",
				})
			} else if out.FestRef != "" {
				warnings = append(warnings, TagParseWarning{
					Field: "fest_ref", Value: seg,
					Reason: "duplicate fest_ref segment",
				})
			} else {
				out.FestRef = seg
			}
			rest = after
		case strings.HasPrefix(rest, "WI-"):
			// WI- used to be the last segment; it no longer is (NT- may follow,
			// since a note captured inside an active workitem carries both).
			// splitWorkitemSegment bounds the match at the fixed ref length so
			// any trailing "-NT-..." is left for the next loop iteration.
			seg, after := splitWorkitemSegment(rest)
			if !tagWorkitemRefRe.MatchString(seg) {
				warnings = append(warnings, TagParseWarning{
					Field: "workitem_ref", Value: seg,
					Reason: "shape check failed (want WI-<6 hex>)",
				})
			} else if out.WorkitemRef != "" {
				warnings = append(warnings, TagParseWarning{
					Field: "workitem_ref", Value: seg,
					Reason: "duplicate workitem_ref segment",
				})
			} else {
				out.WorkitemRef = seg
			}
			rest = after
		case strings.HasPrefix(rest, "NT-"):
			// NT- is always the last segment in the fixed order.
			seg := rest
			if !tagNoteRefRe.MatchString(seg) {
				warnings = append(warnings, TagParseWarning{
					Field: "note_ref", Value: seg,
					Reason: "shape check failed (want NT-<6 hex>)",
				})
			} else if out.NoteRef != "" {
				warnings = append(warnings, TagParseWarning{
					Field: "note_ref", Value: seg,
					Reason: "duplicate note_ref segment",
				})
			} else {
				out.NoteRef = seg
			}
			rest = ""
		default:
			seg, after := splitAtDash(rest)
			warnings = append(warnings, TagParseWarning{
				Field: "unknown", Value: seg,
				Reason: "unknown segment between known prefixes",
			})
			rest = after
		}
	}
	return out, warnings
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
