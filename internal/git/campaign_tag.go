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
	tag := FormatCampaignTag(campaignID)
	if tag == "" {
		return message
	}

	return tag + " " + message
}
