package dungeon

import (
	"errors"
	"strings"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
)

func TestWrapDungeonMoveError_InvalidItemPath(t *testing.T) {
	err := WrapDungeonMoveError(intdungeon.ErrInvalidItemPath, "../secret.md", "archived")
	if err == nil {
		t.Fatal("WrapDungeonMoveError should return an error")
	}
	if !errors.Is(err, intdungeon.ErrInvalidItemPath) {
		t.Fatalf("expected wrapped ErrInvalidItemPath, got: %v", err)
	}

	msg := err.Error()
	for _, want := range []string{
		"invalid item path",
		"direct child",
		"camp dungeon list --triage",
		"camp dungeon list",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error message to contain %q, got: %s", want, msg)
		}
	}
}

func TestDungeonMove_NoCommitFlagRemoved(t *testing.T) {
	if dungeonMoveCmd.Flags().Lookup("no-commit") != nil {
		t.Fatal("dungeon move should always auto-commit; --no-commit must not be registered")
	}
}

func TestShouldInferDungeonMoveTriageMode(t *testing.T) {
	tests := []struct {
		name              string
		itemName          string
		dungeonRootExists bool
		parentEligible    bool
		want              bool
	}{
		{
			name:           "parent item",
			itemName:       "finished.md",
			parentEligible: true,
			want:           true,
		},
		{
			name:              "dungeon root wins",
			itemName:          "same.md",
			dungeonRootExists: true,
			parentEligible:    true,
			want:              false,
		},
		{
			name:           "not an eligible parent item",
			itemName:       "projects",
			parentEligible: false,
			want:           false,
		},
		{
			name:           "path traversal is not inferred",
			itemName:       "../secret.md",
			parentEligible: true,
			want:           false,
		},
		{
			name:           "nested path is not inferred",
			itemName:       "group/finished.md",
			parentEligible: true,
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInferDungeonMoveTriageMode(tt.itemName, tt.dungeonRootExists, tt.parentEligible)
			if got != tt.want {
				t.Fatalf("shouldInferDungeonMoveTriageMode() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWrapDungeonDocsRouteError_InvalidItemPath(t *testing.T) {
	err := wrapDungeonDocsRouteError(intdungeon.ErrInvalidItemPath, "../secret.md", "architecture")
	if err == nil {
		t.Fatal("wrapDungeonDocsRouteError should return an error")
	}
	if !errors.Is(err, intdungeon.ErrInvalidItemPath) {
		t.Fatalf("expected wrapped ErrInvalidItemPath, got: %v", err)
	}

	msg := err.Error()
	for _, want := range []string{
		"invalid item path",
		"direct child",
		"camp dungeon list --triage",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error message to contain %q, got: %s", want, msg)
		}
	}
}
