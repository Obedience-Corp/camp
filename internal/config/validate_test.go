package config

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateCampaignConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *CampaignConfig
		wantErr error
	}{
		{
			name: "valid config",
			cfg: &CampaignConfig{
				Name: "test-campaign",
				Type: CampaignTypeProduct,
			},
			wantErr: nil,
		},
		{
			name:    "missing name",
			cfg:     &CampaignConfig{Type: CampaignTypeProduct},
			wantErr: ErrNameRequired,
		},
		{
			name: "invalid type",
			cfg: &CampaignConfig{
				Name: "test",
				Type: CampaignType("invalid"),
			},
			wantErr: ErrInvalidType,
		},
		{
			name: "name too long",
			cfg: &CampaignConfig{
				Name: strings.Repeat("a", 101),
				Type: CampaignTypeProduct,
			},
			wantErr: ErrNameTooLong,
		},
		{
			name: "name with invalid characters",
			cfg: &CampaignConfig{
				Name: "test/campaign",
				Type: CampaignTypeProduct,
			},
			wantErr: ErrInvalidName,
		},
		{
			name: "name with backslash",
			cfg: &CampaignConfig{
				Name: "test\\campaign",
				Type: CampaignTypeProduct,
			},
			wantErr: ErrInvalidName,
		},
		{
			name: "all valid types",
			cfg: &CampaignConfig{
				Name: "test",
				Type: CampaignTypeResearch,
			},
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCampaignConfig(tt.cfg)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateCampaignConfig() error = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Errorf("ValidateCampaignConfig() error = nil, want %v", tt.wantErr)
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidateCampaignConfig() error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCampaignConfig_WithProjects(t *testing.T) {
	cfg := &CampaignConfig{
		Name: "test",
		Type: CampaignTypeProduct,
		Projects: []ProjectConfig{
			{Name: "project-a", Path: "projects/a"},
			{Name: "", Path: "projects/b"}, // invalid - missing name
		},
	}

	err := ValidateCampaignConfig(cfg)
	if err == nil {
		t.Error("ValidateCampaignConfig() expected error for invalid project")
	}
}

func TestValidateProjectConfig(t *testing.T) {
	tests := []struct {
		name    string
		p       *ProjectConfig
		wantErr bool
	}{
		{
			name: "valid project",
			p: &ProjectConfig{
				Name: "my-project",
				Path: "projects/my-project",
			},
			wantErr: false,
		},
		{
			name: "missing name",
			p: &ProjectConfig{
				Path: "projects/my-project",
			},
			wantErr: true,
		},
		{
			name: "missing path",
			p: &ProjectConfig{
				Name: "my-project",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectConfig(tt.p)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProjectConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateGlobalConfig(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *GlobalConfig
		wantErr bool
	}{
		{
			name:    "empty config is valid",
			cfg:     &GlobalConfig{},
			wantErr: false,
		},
		{
			name: "valid config with all fields",
			cfg: &GlobalConfig{
				DefaultType: CampaignTypeProduct,
				Editor:      "vim",
				NoColor:     true,
			},
			wantErr: false,
		},
		{
			name: "invalid default type",
			cfg: &GlobalConfig{
				DefaultType: CampaignType("invalid"),
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateGlobalConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateGlobalConfig() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateRegisteredCampaign(t *testing.T) {
	tests := []struct {
		name    string
		c       *RegisteredCampaign
		wantErr bool
	}{
		{
			name: "valid registration",
			c: &RegisteredCampaign{
				Path: "/home/user/campaign",
				Type: CampaignTypeProduct,
			},
			wantErr: false,
		},
		{
			name: "missing path",
			c: &RegisteredCampaign{
				Type: CampaignTypeProduct,
			},
			wantErr: true,
		},
		{
			name: "invalid type",
			c: &RegisteredCampaign{
				Path: "/home/user/campaign",
				Type: CampaignType("invalid"),
			},
			wantErr: true,
		},
		{
			name: "empty type is valid",
			c: &RegisteredCampaign{
				Path: "/home/user/campaign",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRegisteredCampaign(tt.c)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRegisteredCampaign() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
