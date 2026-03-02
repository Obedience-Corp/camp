// Package campaign provides campaign detection and management functionality.
package campaign

import (
	"errors"
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// Sentinels marked with %w wrap the canonical sentinel from internal/errors
// to enable cross-package errors.Is() matching.
var (
	// ErrNotInCampaign is returned when the current directory is not inside a campaign.
	ErrNotInCampaign = errors.New("not inside a campaign directory\n" +
		"Hint: Run 'camp init' to create a campaign, or navigate to an existing one")

	// ErrCampaignExists is returned when trying to initialize a campaign that already exists.
	ErrCampaignExists = fmt.Errorf("campaign already exists in this directory: %w", camperrors.ErrAlreadyExists)

	// ErrInvalidCampaign is returned when the campaign directory is corrupted or invalid.
	ErrInvalidCampaign = fmt.Errorf("invalid campaign directory: %w", camperrors.ErrInvalidInput)
)
