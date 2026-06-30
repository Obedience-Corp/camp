package git

import (
	"regexp"
	"strings"

	"github.com/Obedience-Corp/camp/internal/slug"
)

const campaignTagMaxIDLen = 8

// legacyTagMarker is the fixed leading token campaign tags carried before they
// embedded the campaign name. Tags formatted without a resolvable campaign name
// fall back to "[<legacyTagMarker>-<id>...]", and ParseTag still recognizes this
// form so the entire pre-existing commit history resolves correctly.
const legacyTagMarker = "OBEY-CAMPAIGN"

// FormatContextTagsFull composes the consolidated campaign tag from any subset
// of (campaign name, campaign id, quest id, festival ref, workitem ref).
//
// The leading token is the slugified campaign name followed by ":" and the
// short campaign id, e.g. "[obey-campaign:8deed8b4]". When campaignName has no
// usable slug (empty or unslugifiable), it falls back to the legacy
// "[OBEY-CAMPAIGN-<id>]" form. After the leading token the component order is
// fixed: quest id (qst_<...>), then festival ref (FE-<ref>), then workitem ref
// (WI-<ref>). Absent components are omitted entirely; their separators do not
// appear in the output.
//
// Returns "" when campaignID is empty (no tag without a campaign).
func FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef string) string {
	if campaignID == "" {
		return ""
	}
	shortID := campaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}

	head := legacyTagMarker + "-" + shortID
	if nameSlug := slug.Generate(campaignName); nameSlug != "" {
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
		// Workitem refs already carry the WI- prefix per
		// internal/workitem/ref.go::Derive. Embed verbatim so the bracket
		// reads `WI-WI-abcdef`, matching the documented tag grammar.
		parts = append(parts, "WI-"+workitemRef)
	}
	return "[" + strings.Join(parts, "-") + "]"
}

// FormatCampaignTag returns the legacy "[OBEY-CAMPAIGN-{id}]" prefix string for
// callers that only have a campaign id (e.g. the public id-only API consumed by
// fest). If a questID is provided it is appended inside the same bracket:
// "[OBEY-CAMPAIGN-{id}-{questID}]". Truncates campaignID to 8 characters.
// Returns empty string if campaignID is empty.
func FormatCampaignTag(campaignID string, questID ...string) string {
	qid := ""
	if len(questID) > 0 {
		qid = questID[0]
	}
	return FormatContextTagsFull("", campaignID, qid, "", "")
}

// PrependCampaignTag prepends the legacy id-only campaign tag to a commit
// message. If campaignID is empty, returns the message unchanged.
func PrependCampaignTag(campaignID, message string) string {
	return PrependContextTagsFull("", campaignID, "", "", "", message)
}

// FormatContextTags returns the combined campaign/quest tag prefix string.
func FormatContextTags(campaignName, campaignID, questID string) string {
	return FormatContextTagsFull(campaignName, campaignID, questID, "", "")
}

// PrependContextTags prepends the campaign and optional quest tag to a message.
func PrependContextTags(campaignName, campaignID, questID, message string) string {
	return PrependContextTagsFull(campaignName, campaignID, questID, "", "", message)
}

// PrependContextTagsFull prepends the consolidated campaign tag to a commit
// message. If campaignID is empty, returns the message unchanged (no tag
// without a campaign).
func PrependContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef, message string) string {
	tag := FormatContextTagsFull(campaignName, campaignID, questID, festRef, workitemRef)
	if tag == "" {
		return message
	}
	return tag + " " + message
}

// TagComponents are the parsed pieces of a campaign tag. Empty fields indicate
// the component was absent.
type TagComponents struct {
	CampaignID   string `json:"campaign_id"`             // short form, max 8 chars
	CampaignName string `json:"campaign_name,omitempty"` // slug, present only on name-style tags
	QuestID      string `json:"quest_id"`                // quest id component, when present
	FestRef      string `json:"fest_ref"`                // festival ref component, when present
	WorkitemRef  string `json:"workitem_ref"`            // includes the leading "WI-" prefix (e.g. "WI-abcdef")
}

// leadingTagRegex captures the leading bracket content. The campaign-tag
// contract is anchored to position 0: a tag is only what FormatContextTagsFull
// produces at the start of the subject. Embedded mentions in revert subjects,
// code samples, or appended notes are intentionally ignored. The captured
// content is classified as a name-style tag, a legacy tag, or not-a-tag by
// ParseTagDetailed.
var leadingTagRegex = regexp.MustCompile(`^\[([^\]]+)\]`)

// tagBodyScanRegex is the unanchored form, retained for callers that
// intentionally scan commit bodies for tag mentions (e.g. body-grep paths that
// surface "this commit references campaign X" attributions). It matches both
// the name-style and legacy forms. Do NOT use this in ParseTag; the contract
// there is "leading tag only".
var tagBodyScanRegex = regexp.MustCompile(`\[(?:` + legacyTagMarker + `-[^\]]+|[a-z0-9][a-z0-9-]*:[0-9a-f]{1,8}[^\]]*)\]`)

var (
	tagWorkitemRefRe = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)
	tagQuestIDRe     = regexp.MustCompile(`^qst_[A-Za-z0-9_]{1,40}$`)
	tagFestRefRe     = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
	// tagNameStyleIDRe gates the id segment of a name-style tag. Real campaign
	// ids are derived from a UUID, so they are always lowercase hex; requiring
	// that shape lets the parser reject ordinary bracketed prefixes such as
	// "[wip]" or "[scope:msg]" instead of mistaking them for campaign tags.
	tagNameStyleIDRe  = regexp.MustCompile(`^[0-9a-f]{1,8}$`)
	tagCampaignNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// TagParseWarning records a degraded parse: a component whose shape check
// failed (and was zeroed out) or an unknown segment encountered between known
// prefixes. ParseTagDetailed returns these so callers can decide whether to
// log, surface, or treat as a hard error.
type TagParseWarning struct {
	Field  string
	Value  string
	Reason string
}

// ParseTag extracts the components of a campaign tag from a commit subject.
// Returns a zero-valued TagComponents when no tag is present.
//
// ParseTag matches only the leading bracket; embedded mentions in revert
// subjects or code samples are intentionally ignored. Callers that need
// body-grep semantics must use tagBodyScanRegex directly.
//
// ParseTag assumes quest IDs match `qst_[0-9]+_[a-z0-9]+` per
// internal/quest/slug.go. If that alphabet is extended to include "-",
// update the segment walker and add adversarial quest-id cases.
func ParseTag(subject string) TagComponents {
	tc, _ := ParseTagDetailed(subject)
	return tc
}

// ParseTagDetailed is the warnings-aware peer of ParseTag. It recognizes both
// the name-style tag ("[<name>:<id>...]") and the legacy tag
// ("[OBEY-CAMPAIGN-<id>...]"), then walks the inner string once to peel off
// quest, festival, and workitem segments in their fixed order. Each peeled
// segment is anchored on its prefix (`qst_`, `FE-`, `WI-`); whatever is left
// between the previous prefix and the next belongs to the previous segment.
// This matches FormatContextTagsFull's grammar exactly. It continues scanning
// past unknown segments, applies shape checks to extracted component values,
// zeros out failures, and records each fixup in the returned warnings slice.
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
		// A leading bracket that is neither a name-style nor a legacy campaign
		// tag (e.g. "[wip]", "[scope: note]") is not our tag.
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
			// Per the tag grammar, WI- is always the last segment, so the
			// rest of the payload IS the workitem ref. Anything that does
			// not match the canonical shape is rejected wholesale rather
			// than peeled apart into multiple unknown segments.
			payload := rest[len("WI-"):]
			if !tagWorkitemRefRe.MatchString(payload) {
				warnings = append(warnings, TagParseWarning{
					Field: "workitem_ref", Value: payload,
					Reason: "shape check failed (want WI-<6 hex>)",
				})
			} else if out.WorkitemRef != "" {
				warnings = append(warnings, TagParseWarning{
					Field: "workitem_ref", Value: payload,
					Reason: "duplicate workitem_ref segment",
				})
			} else {
				out.WorkitemRef = payload
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

// isNameStyleHead reports whether inner leads with a "<name-slug>:<id>" head,
// i.e. a name-style campaign tag. It requires a non-empty slug name before the
// first colon and a hex id immediately after it so that ordinary bracketed
// subject prefixes are not misread as campaign tags.
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

// splitAtDash returns the substring up to the next "-" and the remainder
// after the dash. If no dash is present, returns (s, "").
func splitAtDash(s string) (string, string) {
	if i := strings.Index(s, "-"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}
