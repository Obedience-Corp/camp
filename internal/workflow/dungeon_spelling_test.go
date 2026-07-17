package workflow

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestService_Init_DungeonSpelling(t *testing.T) {
	tests := []struct {
		name       string
		spelling   string
		wantDir    string
		wantAbsent string
	}{
		{name: "campaign is hidden", spelling: ".dungeon", wantDir: ".dungeon", wantAbsent: "dungeon"},
		{name: "campaign is visible", spelling: "dungeon", wantDir: "dungeon", wantAbsent: ".dungeon"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			dir, _ = filepath.EvalSymlinks(dir)
			svc := NewService(dir, WithDungeonSpelling(tt.spelling))

			if _, err := svc.Init(context.Background(), InitOptions{SchemaVersion: 2}); err != nil {
				t.Fatalf("Init() error = %v", err)
			}

			if _, err := os.Stat(filepath.Join(dir, tt.wantDir)); err != nil {
				t.Errorf("expected %s to exist: %v", tt.wantDir, err)
			}
			if _, err := os.Stat(filepath.Join(dir, tt.wantAbsent)); !os.IsNotExist(err) {
				t.Errorf("expected %s not to exist", tt.wantAbsent)
			}
			if _, err := os.Stat(filepath.Join(dir, tt.wantDir, "completed")); err != nil {
				t.Errorf("expected %s/completed to exist: %v", tt.wantDir, err)
			}
		})
	}
}

func TestService_Init_PreservesEstablishedDungeonSpelling(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Simulate a legacy workflow directory that already has a visible dungeon.
	if err := os.MkdirAll(filepath.Join(dir, "dungeon"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	svc := NewService(dir, WithDungeonSpelling(".dungeon"))
	if _, err := svc.Init(context.Background(), InitOptions{SchemaVersion: 2, Force: true}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, "dungeon")); err != nil {
		t.Errorf("expected existing visible dungeon to be preserved: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".dungeon")); !os.IsNotExist(err) {
		t.Error("expected no hidden dungeon to be created alongside an existing visible one")
	}
}

func TestService_ListAndMove_HiddenDungeon(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	svc := NewService(dir, WithDungeonSpelling(".dungeon"))
	ctx := context.Background()
	if _, err := svc.Init(ctx, InitOptions{SchemaVersion: 2}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}

	itemPath := filepath.Join(dir, "my-item.md")
	if err := os.WriteFile(itemPath, []byte("content"), 0644); err != nil {
		t.Fatalf("write item: %v", err)
	}

	moveResult, err := svc.Move(ctx, "my-item.md", "dungeon/completed", MoveOptions{})
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	wantDestPrefix := filepath.Join(dir, ".dungeon", "completed") + string(filepath.Separator)
	if !strings.HasPrefix(moveResult.DestinationPath, wantDestPrefix) {
		t.Errorf("DestinationPath = %q, want prefix %q", moveResult.DestinationPath, wantDestPrefix)
	}
	if _, err := os.Stat(moveResult.DestinationPath); err != nil {
		t.Errorf("expected moved item to exist at %s: %v", moveResult.DestinationPath, err)
	}

	listResult, err := svc.List(ctx, "dungeon/completed", ListOptions{})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	found := false
	for _, item := range listResult.Items {
		if item.Name == "my-item.md" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected my-item.md in dungeon/completed listing, got %+v", listResult.Items)
	}
}

func TestService_Migrate_DetectsHiddenDungeon(t *testing.T) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)

	// Simulate a pre-existing hidden dungeon with no .workflow.yaml yet.
	if err := os.MkdirAll(filepath.Join(dir, ".dungeon", "completed"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	svc := NewService(dir)
	ctx := context.Background()
	result, err := svc.Migrate(ctx, MigrateOptions{})
	if err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	foundPreserved := false
	for _, p := range result.Preserved {
		if p == ".dungeon/" {
			foundPreserved = true
		}
	}
	if !foundPreserved {
		t.Errorf("expected .dungeon/ to be preserved, got %+v", result.Preserved)
	}

	if _, err := os.Stat(filepath.Join(dir, "dungeon")); !os.IsNotExist(err) {
		t.Error("Migrate() should not create a visible dungeon alongside an existing hidden one")
	}
}
