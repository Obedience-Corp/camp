package theme

import (
	"testing"
)

func TestIsValidTheme(t *testing.T) {
	tests := []struct {
		name     string
		theme    string
		expected bool
	}{
		{"adaptive", "adaptive", true},
		{"light", "light", true},
		{"dark", "dark", true},
		{"high-contrast", "high-contrast", true},
		{"empty", "", false},
		{"invalid", "neon", false},
		{"case sensitive", "Dark", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsValidTheme(tt.theme)
			if result != tt.expected {
				t.Errorf("IsValidTheme(%q) = %v, want %v", tt.theme, result, tt.expected)
			}
		})
	}
}

func TestGetTheme(t *testing.T) {
	tests := []struct {
		name  string
		theme ThemeName
	}{
		{"adaptive", ThemeAdaptive},
		{"light", ThemeLight},
		{"dark", ThemeDark},
		{"high-contrast", ThemeHighContrast},
		{"unknown defaults to adaptive", ThemeName("unknown")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetTheme(tt.theme)
			if result == nil {
				t.Errorf("GetTheme(%q) returned nil", tt.theme)
			}
		})
	}
}

func TestValidThemes(t *testing.T) {
	expected := []ThemeName{ThemeAdaptive, ThemeLight, ThemeDark, ThemeHighContrast}
	if len(ValidThemes) != len(expected) {
		t.Errorf("ValidThemes has %d items, want %d", len(ValidThemes), len(expected))
	}

	for i, theme := range expected {
		if ValidThemes[i] != theme {
			t.Errorf("ValidThemes[%d] = %q, want %q", i, ValidThemes[i], theme)
		}
	}
}
