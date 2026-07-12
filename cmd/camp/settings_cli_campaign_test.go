package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestApplyCampaignScalarKey(t *testing.T) {
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr bool
		check   func(*config.CampaignConfig) bool
	}{
		{"unknown type rejected", settingsKeyLocalCampaignType, "bogus", true, nil},
		{"name trimmed", settingsKeyLocalCampaignName, "  New Name  ", false, func(c *config.CampaignConfig) bool { return c.Name == "New Name" }},
		{"mission set", settingsKeyLocalCampaignMission, "Ship it", false, func(c *config.CampaignConfig) bool { return c.Mission == "Ship it" }},
		{"description set", settingsKeyLocalCampaignDescription, "A thing", false, func(c *config.CampaignConfig) bool { return c.Description == "A thing" }},
		{"type validated", settingsKeyLocalCampaignType, "Research", false, func(c *config.CampaignConfig) bool { return c.Type == config.CampaignTypeResearch }},
		{"commit hook set", settingsKeyLocalCampaignCommitHook, "ob commit", false, func(c *config.CampaignConfig) bool { return c.Hooks.CommitMessage.Command == "ob commit" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.CampaignConfig{ID: "keep-me", Type: config.CampaignTypeProduct}
			_, err := applyCampaignScalarKey(cfg, tt.key, tt.value)
			if (err != nil) != tt.wantErr {
				t.Fatalf("applyCampaignScalarKey(%s, %q) err = %v, wantErr %v", tt.key, tt.value, err, tt.wantErr)
			}
			if cfg.ID != "keep-me" {
				t.Errorf("ID must be preserved, got %q", cfg.ID)
			}
			if !tt.wantErr && tt.check != nil && !tt.check(cfg) {
				t.Errorf("field not set correctly for key %s", tt.key)
			}
		})
	}
}

func TestIsCampaignScalarKey(t *testing.T) {
	for _, key := range []string{
		settingsKeyLocalCampaignName, settingsKeyLocalCampaignType, settingsKeyLocalCampaignCommitHook,
	} {
		if !isCampaignScalarKey(key) {
			t.Errorf("%q should be a campaign scalar key", key)
		}
	}
	for _, key := range []string{settingsKeyGlobalTheme, settingsKeyLocalThemeOverride, "local.campaign.concepts"} {
		if isCampaignScalarKey(key) {
			t.Errorf("%q should not be a campaign scalar key", key)
		}
	}
}
