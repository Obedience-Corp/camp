package config

import camperrors "github.com/Obedience-Corp/camp/internal/errors"

func ValidStatuses() []string {
	return []string{StatusActive, StatusInactive, StatusReference}
}

func ValidateStatus(status string) error {
	switch status {
	case StatusActive, StatusInactive, StatusReference:
		return nil
	}
	return camperrors.NewValidation("status",
		"invalid status \""+status+"\"; must be one of: "+StatusActive+", "+StatusInactive+", "+StatusReference, nil)
}
