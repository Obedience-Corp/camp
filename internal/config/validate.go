package config

import (
	"errors"
	"fmt"
	"strings"
)

// Validation errors
var (
	ErrNameRequired = errors.New("campaign name is required")
	ErrInvalidType  = errors.New("invalid campaign type")
	ErrNameTooLong  = errors.New("campaign name too long (max 100 characters)")
	ErrInvalidName  = errors.New("campaign name contains invalid characters")
)

// ValidateCampaignConfig validates a campaign configuration.
// Returns an error if any required fields are missing or invalid.
func ValidateCampaignConfig(cfg *CampaignConfig) error {
	if cfg.Name == "" {
		return ErrNameRequired
	}

	if len(cfg.Name) > 100 {
		return ErrNameTooLong
	}

	// Check for invalid characters in name
	if strings.ContainsAny(cfg.Name, "/\\:*?\"<>|") {
		return ErrInvalidName
	}

	if !cfg.Type.Valid() {
		return fmt.Errorf("%w: %q (valid: product, research, tools, personal)", ErrInvalidType, cfg.Type)
	}

	// Validate projects if present
	for i, p := range cfg.Projects {
		if err := ValidateProjectConfig(&p); err != nil {
			return fmt.Errorf("project %d: %w", i, err)
		}
	}

	return nil
}

// ValidateProjectConfig validates a project configuration.
func ValidateProjectConfig(p *ProjectConfig) error {
	if p.Name == "" {
		return errors.New("project name is required")
	}
	if p.Path == "" {
		return errors.New("project path is required")
	}
	return nil
}

// ValidateGlobalConfig validates a global configuration.
func ValidateGlobalConfig(cfg *GlobalConfig) error {
	// GlobalConfig only contains user preferences, no validation needed
	return nil
}

// ValidateRegisteredCampaign validates a registered campaign entry.
func ValidateRegisteredCampaign(c *RegisteredCampaign) error {
	if c.Path == "" {
		return errors.New("campaign path is required")
	}
	if c.Type != "" && !c.Type.Valid() {
		return fmt.Errorf("%w: %q", ErrInvalidType, c.Type)
	}
	return nil
}
