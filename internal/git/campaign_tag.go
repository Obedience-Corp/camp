package git

import (
	"regexp"
	"strings"
)

const campaignTagMaxIDLen = 8

// FormatContextTagsFull composes the consolidated campaign tag from any
// subset of the four components. Component order inside the bracket is
// fixed: campaign id, then quest id (qst_<...>), then festival ref
// (FE-<ref>), then workitem ref (WI-<ref>). Absent components are
// omitted entirely; their separators do not appear in the output.
//
// Returns "" when campaignID is empty (no tag without a campaign).
func FormatContextTagsFull(campaignID, questID, festRef, workitemRef string) string {
	if campaignID == "" {
		return ""
	}
	shortID := campaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}
	parts := []string{"OBEY-CAMPAIGN", shortID}
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

// FormatCampaignTag returns the "[OBEY-CAMPAIGN-{id}]" prefix string.
// If a questID is provided, it is appended inside the same bracket:
// "[OBEY-CAMPAIGN-{id}-{questID}]".
// Truncates campaignID to 8 characters. Returns empty string if campaignID is empty.
func FormatCampaignTag(campaignID string, questID ...string) string {
	qid := ""
	if len(questID) > 0 {
		qid = questID[0]
	}
	return FormatContextTagsFull(campaignID, qid, "", "")
}

// PrependCampaignTag prepends the campaign tag to a commit message.
// If campaignID is empty, returns the message unchanged.
func PrependCampaignTag(campaignID, message string) string {
	return PrependContextTags(campaignID, "", message)
}

// FormatContextTags returns the combined campaign/quest tag prefix string.
// When questID is non-empty, produces "[OBEY-CAMPAIGN-{id}-{questID}]".
func FormatContextTags(campaignID, questID string) string {
	return FormatContextTagsFull(campaignID, questID, "", "")
}

// PrependContextTags prepends the campaign and optional quest tag to a message.
func PrependContextTags(campaignID, questID, message string) string {
	tag := FormatContextTags(campaignID, questID)
	if tag == "" {
		return message
	}
	return tag + " " + message
}

// TagComponents are the parsed pieces of a `[OBEY-CAMPAIGN-...]` tag.
// Empty fields indicate the component was absent.
type TagComponents struct {
	CampaignID  string // short form, max 8 chars
	QuestID     string
	FestRef     string
	WorkitemRef string // includes the leading "WI-" prefix (e.g. "WI-abcdef")
}

// leadingTagRegex anchors the tag match to the start of the subject. This is
// the contract ParseTag enforces: a tag is only what FormatContextTagsFull
// produces at position 0. Embedded mentions in revert subjects, code
// samples, or appended notes are intentionally ignored.
var leadingTagRegex = regexp.MustCompile(`^\[OBEY-CAMPAIGN-([^\]]+)\]`)

// tagBodyScanRegex is the unanchored form, retained for callers that
// intentionally scan commit bodies for tag mentions (e.g. body-grep paths
// that surface "this commit references campaign X" attributions). Do NOT
// use this in ParseTag; the contract there is "leading tag only".
var tagBodyScanRegex = regexp.MustCompile(`\[OBEY-CAMPAIGN-([^\]]+)\]`)

var (
	tagWorkitemRefRe = regexp.MustCompile(`^WI-[0-9a-f]{6}$`)
	tagQuestIDRe     = regexp.MustCompile(`^qst_[A-Za-z0-9_]{1,40}$`)
	tagFestRefRe     = regexp.MustCompile(`^[A-Za-z0-9]{1,32}$`)
)

// TagParseWarning records a degraded parse: a component whose shape check
// failed (and was zeroed out) or an unknown segment encountered between
// known prefixes. ParseTagDetailed returns these so callers can decide
// whether to log, surface, or treat as a hard error.
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
// update indexOfNextPrefix and add adversarial quest-id cases.
//
// Two-stage parse: regex strips the bracket and the OBEY-CAMPAIGN prefix,
// then the inner string is walked once to peel off quest, festival, and
// workitem segments in their fixed order. Each peeled segment is anchored
// on its prefix (`qst_`, `FE-`, `WI-`); whatever is left between the
// previous prefix and the next belongs to the previous segment. This
// matches FormatContextTagsFull's grammar exactly.
func ParseTag(subject string) TagComponents {
	tc, _ := ParseTagDetailed(subject)
	return tc
}

// ParseTagDetailed is the warnings-aware peer of ParseTag. It continues
// scanning past unknown segments, applies shape checks to extracted
// component values, zeros out failures, and records each fixup in the
// returned warnings slice.
func ParseTagDetailed(subject string) (TagComponents, []TagParseWarning) {
	m := leadingTagRegex.FindStringSubmatch(subject)
	if m == nil {
		return TagComponents{}, nil
	}
	inner := m[1]
	var warnings []TagParseWarning

	idEnd := strings.Index(inner, "-")
	if idEnd < 0 {
		return TagComponents{CampaignID: inner}, nil
	}
	out := TagComponents{CampaignID: inner[:idEnd]}
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

// splitAtDash returns the substring up to the next "-" and the remainder
// after the dash. If no dash is present, returns (s, "").
func splitAtDash(s string) (string, string) {
	if i := strings.Index(s, "-"); i >= 0 {
		return s[:i], s[i+1:]
	}
	return s, ""
}

