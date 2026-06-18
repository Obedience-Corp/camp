package config

import (
	"regexp"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var orgTagNamePattern = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

func ValidateName(field, name string) error {
	if orgTagNamePattern.MatchString(name) {
		return nil
	}
	return camperrors.NewValidation(field,
		"invalid "+field+" name \""+name+"\": must be lowercase letters, digits, and hyphens with no leading digit", nil)
}
