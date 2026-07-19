package theme

import (
	"reflect"
	"testing"

	"github.com/Obedience-Corp/obey-shared/brand"
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

func TestResolvePaletteUsesSharedBrand(t *testing.T) {
	caps := brand.Capabilities{
		IsTTY:           true,
		ColorDepth:      brand.ColorTrueColor,
		DarkBackground:  true,
		BackgroundKnown: true,
	}

	for _, tc := range []struct {
		name  string
		theme ThemeName
		mode  brand.Mode
	}{
		{name: "dark", theme: ThemeDark, mode: brand.ModeDark},
		{name: "light", theme: ThemeLight, mode: brand.ModeLight},
		{name: "high contrast", theme: ThemeHighContrast, mode: brand.ModeHighContrast},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolvePalette(tc.theme, caps)
			want := brand.Resolve(tc.mode, caps)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("resolvePalette(%q) = %#v, want shared palette %#v", tc.theme, got, want)
			}
		})
	}
}

func TestResolvePaletteHonorsNoColorOverride(t *testing.T) {
	plainOverride.Store(false)
	defer plainOverride.Store(false)

	caps := brand.Capabilities{IsTTY: true, ColorDepth: brand.ColorTrueColor}
	plainOverride.Store(true)
	got := resolvePalette(ThemeDark, caps)
	if got.Mode != brand.ModePlain || got.ColorEnabled {
		t.Fatalf("no-color palette = %#v, want plain palette", got)
	}
}

func TestBackgroundFromColorFGBG(t *testing.T) {
	tests := []struct {
		value string
		dark  bool
		ok    bool
	}{
		{value: "15;0", dark: true, ok: true},
		{value: "15;7", dark: false, ok: true},
		{value: "", dark: false, ok: false},
		{value: "15;unknown", dark: false, ok: false},
	}
	for _, tc := range tests {
		dark, ok := backgroundFromColorFGBG(tc.value)
		if dark != tc.dark || ok != tc.ok {
			t.Errorf("backgroundFromColorFGBG(%q) = (%v, %v), want (%v, %v)", tc.value, dark, ok, tc.dark, tc.ok)
		}
	}
}
