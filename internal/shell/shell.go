// Package shell provides shell integration scripts for camp.
package shell

import (
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// SupportedShells lists all shells with integration support. "sh" is the
// POSIX fallback and covers dash, busybox ash, and anything else that is
// Bourne-family but neither bash nor zsh; it is the shell that ships as
// /bin/sh on minimal and embedded systems.
var SupportedShells = []string{"zsh", "bash", "fish", "sh"}

// Generate produces shell initialization code for the given shell type.
func Generate(shellType string) (string, error) {
	switch shellType {
	case "zsh":
		return generateZsh(), nil
	case "bash":
		return generateBash(), nil
	case "fish":
		return generateFish(), nil
	case "sh":
		return generateSh(), nil
	default:
		return "", camperrors.Newf("unsupported shell: %s (supported: %s)", shellType, strings.Join(SupportedShells, ", "))
	}
}

// IsSupported returns true if the shell type is supported.
func IsSupported(shellType string) bool {
	for _, s := range SupportedShells {
		if s == shellType {
			return true
		}
	}
	return false
}
