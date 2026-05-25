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

// tagShellRegex captures everything between `[OBEY-CAMPAIGN-` and `]`. The
// inner contents are split component-by-component in ParseTag so the parser
// can disambiguate `FE-<ref>` and `WI-<ref>` without relying on regex
// lookahead (which RE2 does not support).
var tagShellRegex = regexp.MustCompile(`\[OBEY-CAMPAIGN-([^\]]+)\]`)

// ParseTag extracts the components of a campaign tag from a commit subject.
// Returns a zero-valued TagComponents when no tag is present.
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
	m := tagShellRegex.FindStringSubmatch(subject)
	if m == nil {
		return TagComponents{}
	}
	inner := m[1]
	// First segment is always the campaign id.
	rest := inner
	out := TagComponents{}
	idEnd := strings.Index(rest, "-")
	if idEnd < 0 {
		out.CampaignID = rest
		return out
	}
	out.CampaignID = rest[:idEnd]
	rest = rest[idEnd+1:]

	// rest may now contain: [qst_<...>-][FE-<ref>-][WI-<ref>]
	for rest != "" {
		switch {
		case strings.HasPrefix(rest, "qst_"):
			end := indexOfNextPrefix(rest)
			out.QuestID = rest[:end]
			rest = trimLeadingSeparator(rest[end:])
		case strings.HasPrefix(rest, "FE-"):
			payload := rest[len("FE-"):]
			end := indexOfNextPrefix(payload)
			out.FestRef = payload[:end]
			rest = trimLeadingSeparator(payload[end:])
		case strings.HasPrefix(rest, "WI-"):
			// Workitem refs in tags are emitted as `WI-<ref>` where the ref
			// itself already starts with `WI-`. Keep the stored ref in its
			// canonical form so the caller does not have to re-add the prefix.
			payload := rest[len("WI-"):]
			out.WorkitemRef = payload
			rest = ""
		default:
			// Unknown trailing content — return what we have rather than guess.
			rest = ""
		}
	}
	return out
}

// indexOfNextPrefix returns the byte offset of the next known segment
// prefix (`-FE-` or `-WI-`) inside s, or len(s) if none is found.
func indexOfNextPrefix(s string) int {
	candidates := []string{"-FE-", "-WI-"}
	best := len(s)
	for _, p := range candidates {
		if idx := strings.Index(s, p); idx >= 0 && idx < best {
			best = idx
		}
	}
	return best
}

func trimLeadingSeparator(s string) string {
	if strings.HasPrefix(s, "-") {
		return s[1:]
	}
	return s
}
