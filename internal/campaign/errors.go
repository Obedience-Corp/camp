// Package campaign provides campaign detection and management functionality.
package campaign

import "errors"

var (
	// ErrNotInCampaign is returned when the current directory is not inside a campaign.
	ErrNotInCampaign = errors.New("not inside a campaign directory")

	// ErrCampaignExists is returned when trying to initialize a campaign that already exists.
	ErrCampaignExists = errors.New("campaign already exists in this directory")

	// ErrInvalidCampaign is returned when the campaign directory is corrupted or invalid.
	ErrInvalidCampaign = errors.New("invalid campaign directory")
)
