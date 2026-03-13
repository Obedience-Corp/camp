package git

import "fmt"

const campaignTagMaxIDLen = 8

// FormatCampaignTag returns the "[OBEY-CAMPAIGN-{id}]" prefix string.
// Truncates campaignID to 8 characters. Returns empty string if campaignID is empty.
func FormatCampaignTag(campaignID string) string {
	if campaignID == "" {
		return ""
	}

	shortID := campaignID
	if len(shortID) > campaignTagMaxIDLen {
		shortID = shortID[:campaignTagMaxIDLen]
	}

	return fmt.Sprintf("[OBEY-CAMPAIGN-%s]", shortID)
}

// PrependCampaignTag prepends the campaign tag to a commit message.
// If campaignID is empty, returns the message unchanged.
func PrependCampaignTag(campaignID, message string) string {
	return PrependContextTags(campaignID, "", message)
}

// FormatQuestTag returns the "[{id}]" quest tag string.
func FormatQuestTag(questID string) string {
	if questID == "" {
		return ""
	}
	return fmt.Sprintf("[%s]", questID)
}

// FormatContextTags returns the combined campaign/quest tag prefix string.
func FormatContextTags(campaignID, questID string) string {
	tag := FormatCampaignTag(campaignID)
	if questTag := FormatQuestTag(questID); questTag != "" {
		tag += questTag
	}
	return tag
}

// PrependContextTags prepends the campaign and optional quest tag to a message.
func PrependContextTags(campaignID, questID, message string) string {
	tag := FormatContextTags(campaignID, questID)
	if tag == "" {
		return message
	}
	return tag + " " + message
}
