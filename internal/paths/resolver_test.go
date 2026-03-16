package paths

import (
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestNewResolverFromConfig_IntentsUsesConfiguredPath(t *testing.T) {
	t.Run("default canonical path", func(t *testing.T) {
		cfg := config.DefaultCampaignConfig("test-campaign")
		resolver := NewResolverFromConfig("/campaign", &cfg)

		if got := resolver.RelativeIntents(); got != ".campaign/intents/" {
			t.Fatalf("RelativeIntents() = %q, want %q", got, ".campaign/intents/")
		}
		if got := resolver.Intents(); got != filepath.Join("/campaign", ".campaign/intents/") {
			t.Fatalf("Intents() = %q, want %q", got, filepath.Join("/campaign", ".campaign/intents/"))
		}
	})

	t.Run("respects configured override", func(t *testing.T) {
		cfg := &config.CampaignConfig{
			Name: "test-campaign",
			Jumps: &config.JumpsConfig{
				Paths: config.CampaignPaths{
					Intents: "custom/intents/",
				},
			},
		}
		cfg.Jumps.ApplyDefaults()

		resolver := NewResolverFromConfig("/campaign", cfg)

		if got := resolver.RelativeIntents(); got != "custom/intents/" {
			t.Fatalf("RelativeIntents() = %q, want %q", got, "custom/intents/")
		}
		if got := resolver.Intents(); got != filepath.Join("/campaign", "custom/intents/") {
			t.Fatalf("Intents() = %q, want %q", got, filepath.Join("/campaign", "custom/intents/"))
		}
	})
}
