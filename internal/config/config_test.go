package config

import "testing"

func TestLoad(t *testing.T) {
	// Test that Load returns a non-nil config
	cfg, err := Load("")
	if err != nil {
		t.Errorf("Load() error = %v; want nil", err)
	}

	if cfg == nil {
		t.Error("Load() returned nil config; want non-nil")
	}
}

func TestConfigSave(t *testing.T) {
	cfg := &CampaignConfig{
		Name:        "test-campaign",
		Description: "Test campaign",
	}

	// Save is a placeholder, so it should return nil
	err := cfg.Save("/tmp/test.yaml")
	if err != nil {
		t.Errorf("Save() error = %v; want nil", err)
	}
}

func TestConfigAlias(t *testing.T) {
	// Test that Config alias works with CampaignConfig
	var cfg Config
	cfg.Name = "alias-test"
	cfg.Type = CampaignTypeProduct

	if cfg.Name != "alias-test" {
		t.Errorf("Config.Name = %s; want alias-test", cfg.Name)
	}
	if cfg.Type != CampaignTypeProduct {
		t.Errorf("Config.Type = %s; want product", cfg.Type)
	}
}
