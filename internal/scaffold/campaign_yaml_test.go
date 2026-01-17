package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/obediencecorp/camp/internal/config"
)

func TestValidateCampaignName(t *testing.T) {
	tests := []struct {
		name   string
		valid  bool
		reason string
	}{
		{"my-campaign", true, ""},
		{"campaign1", true, ""},
		{"a", true, ""},
		{"1", true, ""},
		{"my-campaign-2", true, ""},

		// Invalid names
		{"", false, "empty"},
		{"-invalid", false, "starts with hyphen"},
		{"invalid-", false, "ends with hyphen"},
		{"My-Campaign", false, "uppercase"},
		{"my_campaign", false, "underscore"},
		{"my campaign", false, "space"},
		{"my/campaign", false, "slash"},
		{"my.campaign", false, "dot"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCampaignName(tt.name)
			if tt.valid && err != nil {
				t.Errorf("ValidateCampaignName(%q) returned error: %v, want nil", tt.name, err)
			}
			if !tt.valid && err == nil {
				t.Errorf("ValidateCampaignName(%q) returned nil, want error (%s)", tt.name, tt.reason)
			}
		})
	}
}

func TestValidateCampaignName_TooLong(t *testing.T) {
	longName := ""
	for i := 0; i < 101; i++ {
		longName += "a"
	}

	err := ValidateCampaignName(longName)
	if err == nil {
		t.Error("ValidateCampaignName() should reject names over 100 characters")
	}
}

func TestIsValidCampaignName(t *testing.T) {
	if !IsValidCampaignName("valid-name") {
		t.Error("IsValidCampaignName(\"valid-name\") = false, want true")
	}
	if IsValidCampaignName("Invalid_Name") {
		t.Error("IsValidCampaignName(\"Invalid_Name\") = true, want false")
	}
}

func TestNormalizeCampaignName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"My Campaign", "my-campaign"},
		{"my_campaign", "my-campaign"},
		{"My-Campaign", "my-campaign"},
		{"UPPERCASE", "uppercase"},
		{"with  spaces", "with-spaces"},
		{"already-valid", "already-valid"},
		{"remove/special*chars", "removespecialchars"},
		{"--leading-trailing--", "leading-trailing"},
		{"multiple---hyphens", "multiple-hyphens"},
		{"123numbers", "123numbers"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := NormalizeCampaignName(tt.input)
			if got != tt.expected {
				t.Errorf("NormalizeCampaignName(%q) = %q, want %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestCreateCampaignConfig(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	// Create .campaign directory
	campaignDir := filepath.Join(tmpDir, config.CampaignDir)
	if err := os.MkdirAll(campaignDir, 0755); err != nil {
		t.Fatalf("failed to create campaign dir: %v", err)
	}

	ctx := context.Background()
	opts := InitOptions{
		Name: "test-campaign",
		Type: config.CampaignTypeResearch,
	}

	cfg, err := CreateCampaignConfig(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("CreateCampaignConfig() error = %v", err)
	}

	if cfg.Name != "test-campaign" {
		t.Errorf("cfg.Name = %q, want %q", cfg.Name, "test-campaign")
	}
	if cfg.Type != config.CampaignTypeResearch {
		t.Errorf("cfg.Type = %q, want %q", cfg.Type, config.CampaignTypeResearch)
	}
	if cfg.CreatedAt.IsZero() {
		t.Error("cfg.CreatedAt should not be zero")
	}

	// Verify file was created
	configPath := config.CampaignConfigPath(tmpDir)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("campaign.yaml file was not created")
	}

	// Load and verify round-trip
	loaded, err := config.LoadCampaignConfig(ctx, tmpDir)
	if err != nil {
		t.Fatalf("LoadCampaignConfig() error = %v", err)
	}

	if loaded.Name != cfg.Name {
		t.Errorf("loaded.Name = %q, want %q", loaded.Name, cfg.Name)
	}
}

func TestCreateCampaignConfig_DefaultType(t *testing.T) {
	tmpDir := t.TempDir()
	tmpDir, _ = filepath.EvalSymlinks(tmpDir)

	campaignDir := filepath.Join(tmpDir, config.CampaignDir)
	os.MkdirAll(campaignDir, 0755)

	ctx := context.Background()
	opts := InitOptions{
		Name: "default-type",
		// Type is empty
	}

	cfg, err := CreateCampaignConfig(ctx, tmpDir, opts)
	if err != nil {
		t.Fatalf("CreateCampaignConfig() error = %v", err)
	}

	if cfg.Type != config.CampaignTypeProduct {
		t.Errorf("cfg.Type = %q, want %q (default)", cfg.Type, config.CampaignTypeProduct)
	}
}

func TestCreateCampaignConfig_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := CreateCampaignConfig(ctx, "/some/path", InitOptions{Name: "test"})
	if err != context.Canceled {
		t.Errorf("CreateCampaignConfig() error = %v, want %v", err, context.Canceled)
	}
}

func TestCreateCampaignConfig_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(1 * time.Millisecond)

	_, err := CreateCampaignConfig(ctx, "/some/path", InitOptions{Name: "test"})
	if err != context.DeadlineExceeded {
		t.Errorf("CreateCampaignConfig() error = %v, want %v", err, context.DeadlineExceeded)
	}
}

func TestCampaignNameError(t *testing.T) {
	err := &CampaignNameError{Name: "test", Reason: "test reason"}
	expected := `invalid campaign name "test": test reason`
	if err.Error() != expected {
		t.Errorf("Error() = %q, want %q", err.Error(), expected)
	}
}
