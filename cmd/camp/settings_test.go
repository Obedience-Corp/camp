package main

import (
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestApplyCampaignsDirCandidate(t *testing.T) {
	t.Run("trims and applies value", func(t *testing.T) {
		cfg := config.DefaultGlobalConfig()

		if err := applyCampaignsDirCandidate(&cfg, "  /tmp/campaigns  "); err != nil {
			t.Fatalf("applyCampaignsDirCandidate() unexpected error: %v", err)
		}
		if got := cfg.CampaignsDir; got != "/tmp/campaigns" {
			t.Fatalf("CampaignsDir = %q, want %q", got, "/tmp/campaigns")
		}
	})

	t.Run("empty clears to default sentinel", func(t *testing.T) {
		cfg := config.DefaultGlobalConfig()
		cfg.CampaignsDir = "/tmp/campaigns"

		if err := applyCampaignsDirCandidate(&cfg, "   "); err != nil {
			t.Fatalf("applyCampaignsDirCandidate() unexpected error: %v", err)
		}
		if got := cfg.CampaignsDir; got != "" {
			t.Fatalf("CampaignsDir = %q, want empty", got)
		}
	})
}

func TestApplyCampaignsDirCandidate_InvalidDoesNotMutate(t *testing.T) {
	cfg := config.DefaultGlobalConfig()
	cfg.CampaignsDir = "/tmp/original"

	if err := applyCampaignsDirCandidate(&cfg, "/tmp/bad\x00path"); err == nil {
		t.Fatal("applyCampaignsDirCandidate() expected validation error, got nil")
	}
	if got := cfg.CampaignsDir; got != "/tmp/original" {
		t.Fatalf("CampaignsDir mutated on invalid input: got %q, want %q", got, "/tmp/original")
	}
}
