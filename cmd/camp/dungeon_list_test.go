package main

import "testing"

func TestBuildDungeonListContextLine(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		parentRel  string
		dungeonRel string
		want       string
	}{
		{
			name:       "dungeon mode",
			source:     "dungeon",
			parentRel:  "workflow/design",
			dungeonRel: "workflow/design/dungeon",
			want:       "Context: dungeon=workflow/design/dungeon",
		},
		{
			name:       "triage mode",
			source:     "triage",
			parentRel:  "workflow/design",
			dungeonRel: "workflow/design/dungeon",
			want:       "Context: parent=workflow/design -> dungeon=workflow/design/dungeon",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDungeonListContextLine(tt.source, tt.parentRel, tt.dungeonRel)
			if got != tt.want {
				t.Fatalf("buildDungeonListContextLine() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildDungeonEmptyMessage(t *testing.T) {
	tests := []struct {
		name       string
		source     string
		dungeonRel string
		want       string
	}{
		{
			name:       "dungeon mode",
			source:     "dungeon",
			dungeonRel: "workflow/design/dungeon",
			want:       "Dungeon is empty (context: workflow/design/dungeon).",
		},
		{
			name:       "triage mode",
			source:     "triage",
			dungeonRel: "workflow/design/dungeon",
			want:       "No parent items eligible for triage (context: workflow/design/dungeon).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildDungeonEmptyMessage(tt.source, tt.dungeonRel)
			if got != tt.want {
				t.Fatalf("buildDungeonEmptyMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}
