package config_test

import (
	"context"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	"github.com/Obedience-Corp/camp/internal/config"
)

// The commented hooks block camp writes into every new campaign.yaml is the
// only place most users ever read what the writer timeout is, and it said 5m
// for as long as the default was 12m. A scaffolded config that misstates a
// bound is worse than no comment: it is camp telling the user a number camp
// does not use, in a file the user will edit believing it.
//
// An external test package so it can name autowrite.DefaultWriterTimeout,
// which config itself must not import.
func TestScaffoldedHooksPlaceholderStatesTheRealWriterTimeout(t *testing.T) {
	root := t.TempDir()
	cfg := &config.CampaignConfig{Name: "placeholder-drift"}
	if err := config.SaveCampaignConfig(context.Background(), root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig() error = %v", err)
	}

	written, err := os.ReadFile(config.CampaignConfigPath(root))
	if err != nil {
		t.Fatalf("read scaffolded config: %v", err)
	}
	text := string(written)

	// Compared as durations, not as text: the constant renders "12m0s" and the
	// comment reasonably says "12m". What must not drift is the value.
	durations := regexp.MustCompile(`(?:timeout:|Default)\s+([0-9]+[a-z0-9]*)`).FindAllStringSubmatch(text, -1)
	if len(durations) == 0 {
		t.Fatalf("scaffolded config states no writer timeout at all:\n%s", text)
	}
	for _, match := range durations {
		stated, err := time.ParseDuration(match[1])
		if err != nil {
			t.Errorf("scaffolded config states %q, which is not a duration camp could parse", match[1])
			continue
		}
		if stated != autowrite.DefaultWriterTimeout {
			t.Errorf("scaffolded config says %s, but autowrite.DefaultWriterTimeout is %s",
				match[1], autowrite.DefaultWriterTimeout)
		}
	}
}
