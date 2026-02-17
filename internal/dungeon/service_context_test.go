package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestService_MoveToStatus_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = svc.MoveToStatus(ctx, "item", "status")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestService_MoveToDungeonStatus_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = svc.MoveToDungeonStatus(ctx, "item", tmpDir, "archived")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestService_ListStatusDirs_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ListStatusDirs(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestService_ListItems_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ListItems(ctx)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestService_ListParentItems_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = svc.ListParentItems(ctx, tmpDir)
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestService_AppendCrawlLog_ContextCancelled(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	svc := NewService(tmpDir, filepath.Join(tmpDir, "dungeon"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = svc.AppendCrawlLog(ctx, CrawlEntry{Item: "test", Decision: DecisionKeep})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}
