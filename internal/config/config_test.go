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
	cfg := &Config{
		Name:        "test-campaign",
		Description: "Test campaign",
	}

	// Save is a placeholder, so it should return nil
	err := cfg.Save("/tmp/test.yaml")
	if err != nil {
		t.Errorf("Save() error = %v; want nil", err)
	}
}

func TestConfigTypes(t *testing.T) {
	// Test that Config struct can be instantiated
	cfg := Config{
		Name:        "my-campaign",
		Description: "My campaign description",
		Projects: []ProjectConfig{
			{
				Name: "project-a",
				Path: "projects/project-a",
				URL:  "https://github.com/example/project-a",
			},
		},
	}

	if cfg.Name != "my-campaign" {
		t.Errorf("Config.Name = %s; want my-campaign", cfg.Name)
	}

	if len(cfg.Projects) != 1 {
		t.Errorf("len(Config.Projects) = %d; want 1", len(cfg.Projects))
	}

	if cfg.Projects[0].Name != "project-a" {
		t.Errorf("Config.Projects[0].Name = %s; want project-a", cfg.Projects[0].Name)
	}
}
