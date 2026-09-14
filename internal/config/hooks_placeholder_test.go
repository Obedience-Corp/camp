package config_test

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/autowrite"
	"github.com/Obedience-Corp/camp/internal/config"
)

// The commented hooks block camp writes into every new campaign.yaml is the
// only place most users ever read what the writer's bounds are, and it said 5m
// for as long as the timeout default was 12m. A scaffolded config that
// misstates a bound is worse than no comment: it is camp telling the user a
// number camp does not use, in a file the user will edit believing it.
//
// Both bounds are checked the same way, because the second one (how long camp
// waits for a writer that is down) decides how long a user's commit sits
// unlanded, which is at least as expensive to be wrong about.
//
// An external test package so it can name autowrite's defaults, which config
// itself must not import.
func TestScaffoldedHooksPlaceholderStatesTheRealWriterBounds(t *testing.T) {
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

	defaults := map[string]time.Duration{
		"timeout":      autowrite.DefaultWriterTimeout,
		"retry_window": autowrite.DefaultRetryWindow,
	}
	stated := statedDurationsByField(text)
	for field, want := range defaults {
		got := stated[field]
		if len(got) == 0 {
			t.Errorf("scaffolded config states no %s at all:\n%s", field, text)
			continue
		}
		for _, raw := range got {
			// Compared as durations, not as text: the constant renders "12m0s"
			// and the comment reasonably says "12m". What must not drift is
			// the value.
			d, err := time.ParseDuration(raw)
			if err != nil {
				t.Errorf("scaffolded config states %s %q, which is not a duration camp could parse",
					field, raw)
				continue
			}
			if d != want {
				t.Errorf("scaffolded config says %s %s, but camp's default is %s", field, raw, want)
			}
		}
	}
}

// durationToken matches a duration in the prose or the value of the commented
// block: "Default 12m.", "retry_window: 1h".
var durationToken = regexp.MustCompile(`(?:Default|:)\s+"?([0-9]+[a-z]+[0-9a-z]*)"?`)

// statedDurationsByField attributes every duration in the placeholder to the
// setting it describes.
//
// A setting's comment precedes it, so durations accumulate until a line names
// a field and then belong to that field. Written this way rather than as one
// regex per field because the failure being guarded against is a new setting
// arriving with its own default and nothing noticing that the old check only
// ever looked at the first one.
func statedDurationsByField(text string) map[string][]string {
	out := map[string][]string{}
	var pending []string
	for _, line := range strings.Split(text, "\n") {
		for _, match := range durationToken.FindAllStringSubmatch(line, -1) {
			pending = append(pending, match[1])
		}
		for _, field := range []string{"timeout", "retry_window"} {
			if strings.Contains(line, field+":") {
				out[field] = append(out[field], pending...)
				pending = nil
				break
			}
		}
	}
	return out
}
