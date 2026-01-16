// Package shell provides shell integration scripts for camp.
package shell

import "fmt"

// SupportedShells lists all shells with integration support.
var SupportedShells = []string{"zsh", "bash", "fish"}

// Generate produces shell initialization code for the given shell type.
func Generate(shellType string) (string, error) {
	switch shellType {
	case "zsh":
		return generateZsh(), nil
	case "bash":
		return generateBash(), nil
	case "fish":
		return generateFish(), nil
	default:
		return "", fmt.Errorf("unsupported shell: %s (supported: zsh, bash, fish)", shellType)
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

