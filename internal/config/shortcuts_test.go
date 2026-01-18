package config

import "testing"

func TestShortcutConfig_IsNavigation(t *testing.T) {
	tests := []struct {
		name     string
		shortcut ShortcutConfig
		want     bool
	}{
		{
			name: "path only",
			shortcut: ShortcutConfig{
				Path: "projects/api",
			},
			want: true,
		},
		{
			name: "command only",
			shortcut: ShortcutConfig{
				Command: "just build",
			},
			want: false,
		},
		{
			name: "both path and command",
			shortcut: ShortcutConfig{
				Path:    "projects/api",
				Command: "just build",
			},
			want: true,
		},
		{
			name:     "neither path nor command",
			shortcut: ShortcutConfig{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.shortcut.IsNavigation(); got != tt.want {
				t.Errorf("IsNavigation() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestShortcutConfig_IsCommand(t *testing.T) {
	tests := []struct {
		name     string
		shortcut ShortcutConfig
		want     bool
	}{
		{
			name: "command only",
			shortcut: ShortcutConfig{
				Command: "just build",
			},
			want: true,
		},
		{
			name: "path only",
			shortcut: ShortcutConfig{
				Path: "projects/api",
			},
			want: false,
		},
		{
			name: "both path and command",
			shortcut: ShortcutConfig{
				Path:    "projects/api",
				Command: "just build",
			},
			want: true,
		},
		{
			name:     "neither path nor command",
			shortcut: ShortcutConfig{},
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.shortcut.IsCommand(); got != tt.want {
				t.Errorf("IsCommand() = %v, want %v", got, tt.want)
			}
		})
	}
}
