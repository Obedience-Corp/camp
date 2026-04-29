package dungeon

import (
	"errors"
	"strings"
	"testing"

	intdungeon "github.com/Obedience-Corp/camp/internal/dungeon"
)

func TestWrapDungeonMoveError_InvalidItemPath(t *testing.T) {
	err := wrapDungeonMoveError(intdungeon.ErrInvalidItemPath, "../secret.md", "archived")
	if err == nil {
		t.Fatal("wrapDungeonMoveError should return an error")
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
