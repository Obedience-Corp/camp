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

func TestFindFirstPositionalArg(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		wantName string
		wantIdx  int
	}{
		{
			name:     "no args",
			args:     []string{"camp"},
			wantName: "",
			wantIdx:  0,
		},
		{
			name:     "simple subcommand",
			args:     []string{"camp", "graph"},
			wantName: "graph",
			wantIdx:  1,
		},
		{
			name:     "bool flag then subcommand",
			args:     []string{"camp", "--no-color", "graph"},
			wantName: "graph",
			wantIdx:  2,
		},
		{
			name:     "removed config flag no longer consumes separate value",
			args:     []string{"camp", "--config", "/tmp/camp.json", "graph"},
			wantName: "/tmp/camp.json",
			wantIdx:  2,
		},
		{
			name:     "multiple flags then subcommand",
			args:     []string{"camp", "--no-color", "graph", "build"},
			wantName: "graph",
			wantIdx:  2,
		},
		{
			name:     "double dash terminates flags",
			args:     []string{"camp", "--", "graph"},
			wantName: "graph",
			wantIdx:  2,
		},
		{
			name:     "double dash with no following arg",
			args:     []string{"camp", "--"},
			wantName: "",
			wantIdx:  0,
		},
		{
			name:     "only flags no subcommand",
			args:     []string{"camp", "--no-color"},
			wantName: "",
			wantIdx:  0,
		},
		{
			name:     "unknown flag value is positional",
			args:     []string{"camp", "--config", "myfile"},
			wantName: "myfile",
			wantIdx:  2,
		},
		{
			name:     "unknown flag treated as flag not subcommand",
			args:     []string{"camp", "--unknown", "graph"},
			wantName: "graph",
			wantIdx:  2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotIdx := findFirstPositionalArg(tt.args)
			if gotName != tt.wantName || gotIdx != tt.wantIdx {
				t.Errorf("findFirstPositionalArg(%v) = (%q, %d), want (%q, %d)",
					tt.args, gotName, gotIdx, tt.wantName, tt.wantIdx)
			}
		})
	}
}
