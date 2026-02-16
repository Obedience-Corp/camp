package git

import "testing"

func TestFormatCampaignTag(t *testing.T) {
	tests := []struct {
		name       string
		campaignID string
		want       string
	}{
		{
			name:       "empty ID returns empty string",
			campaignID: "",
			want:       "",
		},
		{
			name:       "short ID used as-is",
			campaignID: "abcd",
			want:       "[OBEY-CAMPAIGN-abcd]",
		},
		{
			name:       "exactly 8 chars not truncated",
			campaignID: "abcdef12",
			want:       "[OBEY-CAMPAIGN-abcdef12]",
		},
		{
			name:       "long ID truncated to 8 chars",
			campaignID: "abcdef12-3456-7890-abcd-ef1234567890",
			want:       "[OBEY-CAMPAIGN-abcdef12]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatCampaignTag(tt.campaignID)
			if got != tt.want {
				t.Errorf("FormatCampaignTag(%q) = %q, want %q", tt.campaignID, got, tt.want)
			}
		})
	}
}

func TestPrependCampaignTag(t *testing.T) {
	tests := []struct {
		name       string
		campaignID string
		message    string
		want       string
	}{
		{
			name:       "empty ID returns message unchanged",
			campaignID: "",
			message:    "Fix bug",
			want:       "Fix bug",
		},
		{
			name:       "prepends tag to message",
			campaignID: "abcdef12-3456-7890",
			message:    "Add new feature",
			want:       "[OBEY-CAMPAIGN-abcdef12] Add new feature",
		},
		{
			name:       "empty message with valid ID",
			campaignID: "abcdef12",
			message:    "",
			want:       "[OBEY-CAMPAIGN-abcdef12] ",
		},
		{
			name:       "short ID preserved",
			campaignID: "abc",
			message:    "Update deps",
			want:       "[OBEY-CAMPAIGN-abc] Update deps",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PrependCampaignTag(tt.campaignID, tt.message)
			if got != tt.want {
				t.Errorf("PrependCampaignTag(%q, %q) = %q, want %q",
					tt.campaignID, tt.message, got, tt.want)
			}
		})
	}
}
