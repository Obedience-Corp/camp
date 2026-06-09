package config

import (
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestIntentTags_ReturnsConfiguredWhenSet(t *testing.T) {
	cfg := &CampaignConfig{Intents: IntentsConfig{Tags: []string{"alpha", "beta"}}}
	got := cfg.IntentTags()
	want := []string{"alpha", "beta"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IntentTags() = %v, want %v", got, want)
	}
}

func TestIntentTags_ReturnsDefaultsWhenUnset(t *testing.T) {
	cfg := &CampaignConfig{}
	got := cfg.IntentTags()
	if !reflect.DeepEqual(got, DefaultIntentTags()) {
		t.Errorf("IntentTags() = %v, want defaults %v", got, DefaultIntentTags())
	}
	if len(got) == 0 {
		t.Error("default intent tags should be non-empty")
	}
}

func TestIntentsConfig_RoundTrips(t *testing.T) {
	cfg := &CampaignConfig{
		ID:      "id",
		Name:    "c",
		Type:    CampaignTypeProduct,
		Intents: IntentsConfig{Tags: []string{"personal", "follow-up"}},
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var got CampaignConfig
	if err := yaml.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !reflect.DeepEqual(got.Intents.Tags, cfg.Intents.Tags) {
		t.Errorf("round-trip tags = %v, want %v", got.Intents.Tags, cfg.Intents.Tags)
	}
}
