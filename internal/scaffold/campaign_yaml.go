package scaffold

import (
	"context"
	"fmt"
	"regexp"
	"time"

	"github.com/obediencecorp/camp/internal/config"
)

// ValidCampaignNameRegex defines the pattern for valid campaign names.
// Names must be lowercase letters, numbers, and hyphens.
// Cannot start or end with a hyphen.
var ValidCampaignNameRegex = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*[a-z0-9]$|^[a-z0-9]$`)

// CampaignNameError is returned when a campaign name is invalid.
type CampaignNameError struct {
	Name   string
	Reason string
}

func (e *CampaignNameError) Error() string {
	return fmt.Sprintf("invalid campaign name %q: %s", e.Name, e.Reason)
}

// ValidateCampaignName checks if the name is valid for a campaign.
// Valid names: lowercase letters, numbers, and hyphens.
// Cannot start or end with a hyphen.
func ValidateCampaignName(name string) error {
	if name == "" {
		return &CampaignNameError{Name: name, Reason: "name cannot be empty"}
	}

	if len(name) > 100 {
		return &CampaignNameError{Name: name, Reason: "name too long (max 100 characters)"}
	}

	if name[0] == '-' {
		return &CampaignNameError{Name: name, Reason: "name cannot start with a hyphen"}
	}

	if name[len(name)-1] == '-' {
		return &CampaignNameError{Name: name, Reason: "name cannot end with a hyphen"}
	}

	if !ValidCampaignNameRegex.MatchString(name) {
		return &CampaignNameError{
			Name:   name,
			Reason: "use only lowercase letters, numbers, and hyphens",
		}
	}

	return nil
}

// IsValidCampaignName returns true if the name is valid for a campaign.
func IsValidCampaignName(name string) bool {
	return ValidateCampaignName(name) == nil
}

// CreateCampaignConfig generates a new campaign.yaml configuration.
func CreateCampaignConfig(ctx context.Context, campaignRoot string, opts InitOptions) (*config.CampaignConfig, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cfg := &config.CampaignConfig{
		Name:        opts.Name,
		Type:        opts.Type,
		CreatedAt:   time.Now().UTC(),
		Description: fmt.Sprintf("Campaign: %s", opts.Name),
		Paths:       config.DefaultCampaignPaths(),
	}

	// Apply defaults
	if cfg.Type == "" {
		cfg.Type = config.CampaignTypeProduct
	}

	// Save to file
	if err := config.SaveCampaignConfig(ctx, campaignRoot, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// NormalizeCampaignName converts a string to a valid campaign name.
// Converts to lowercase, replaces spaces and underscores with hyphens,
// and removes other invalid characters.
func NormalizeCampaignName(name string) string {
	result := make([]byte, 0, len(name))

	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'A' && c <= 'Z':
			result = append(result, c+32) // Convert to lowercase
		case c >= 'a' && c <= 'z':
			result = append(result, c)
		case c >= '0' && c <= '9':
			result = append(result, c)
		case c == ' ' || c == '_':
			result = append(result, '-')
		case c == '-':
			result = append(result, c)
			// Skip other characters
		}
	}

	// Remove leading/trailing hyphens
	s := string(result)
	for len(s) > 0 && s[0] == '-' {
		s = s[1:]
	}
	for len(s) > 0 && s[len(s)-1] == '-' {
		s = s[:len(s)-1]
	}

	// Remove consecutive hyphens
	for i := 0; i < len(s)-1; i++ {
		if s[i] == '-' && s[i+1] == '-' {
			s = s[:i] + s[i+1:]
			i--
		}
	}

	return s
}
