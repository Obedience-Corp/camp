package main

import "testing"

func TestIsKnownCommand(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		// Registered subcommand names
		{"project", true},
		{"cache", true},
		{"skills", true},
		{"intent", true},
		{"dungeon", true},
		{"plugins", true},

		// Aliases (run.go registers "r" as alias for "run")
		{"r", true},

		// Unknown names — should fall through to plugin dispatch
		{"graph", false},
		{"foo", false},
		{"bar", false},
		{"nonexistent", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isKnownCommand(tt.name)
			if got != tt.want {
				t.Errorf("isKnownCommand(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}
