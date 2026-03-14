package git

import "fmt"

const campaignTagMaxIDLen = 8

// FormatCampaignTag returns the "[OBEY-CAMPAIGN-{id}]" prefix string.
// If a questID is provided, it is appended inside the same bracket:
// "[OBEY-CAMPAIGN-{id}-{questID}]".
// Truncates campaignID to 8 characters. Returns empty string if campaignID is empty.
func FormatCampaignTag(campaignID string, questID ...string) string {
	if campaignID == "" {
		return ""
	}

	shortID := campaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}

	if len(questID) > 0 && questID[0] != "" {
		return fmt.Sprintf("[OBEY-CAMPAIGN-%s-%s]", shortID, questID[0])
	}
	return fmt.Sprintf("[OBEY-CAMPAIGN-%s]", shortID)
}

// PrependCampaignTag prepends the campaign tag to a commit message.
// If campaignID is empty, returns the message unchanged.
func PrependCampaignTag(campaignID, message string) string {
	return PrependContextTags(campaignID, "", message)
}

// FormatContextTags returns the combined campaign/quest tag prefix string.
// When questID is non-empty, produces "[OBEY-CAMPAIGN-{id}-{questID}]".
func FormatContextTags(campaignID, questID string) string {
	return FormatCampaignTag(campaignID, questID)
}

// PrependContextTags prepends the campaign and optional quest tag to a message.
func PrependContextTags(campaignID, questID, message string) string {
	tag := FormatContextTags(campaignID, questID)
	if tag == "" {
		return message
	}
	return tag + " " + message
}
