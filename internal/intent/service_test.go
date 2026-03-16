package intent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewIntentService(t *testing.T) {
	svc := NewIntentService("/test/root", "/test/root/intents")
	if svc == nil {
		t.Fatal("NewIntentService returned nil")
	}
	if svc.campaignRoot != "/test/root" {
		t.Errorf("campaignRoot = %q, want %q", svc.campaignRoot, "/test/root")
	}
	if svc.intentsDir != "/test/root/intents" {
		t.Errorf("intentsDir = %q, want %q", svc.intentsDir, "/test/root/intents")
	}
}

func TestIntentService_CreateDirect(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	tests := []struct {
		name    string
		opts    CreateOptions
		wantErr bool
		checks  func(t *testing.T, intent *Intent)
	}{
		{
			name: "basic creation",
			opts: CreateOptions{
				Title:     "Test Intent",
				Type:      TypeFeature,
				Concept:   "test-project",
				Author:    "tester",
				Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
			},
			wantErr: false,
			checks: func(t *testing.T, intent *Intent) {
				if intent.Title != "Test Intent" {
					t.Errorf("Title = %q, want %q", intent.Title, "Test Intent")
				}
				if intent.Status != StatusInbox {
					t.Errorf("Status = %q, want %q", intent.Status, StatusInbox)
				}
				if !strings.Contains(intent.Path, "inbox") {
					t.Errorf("Path should contain 'inbox', got %q", intent.Path)
				}
				// Verify file exists
				if _, err := os.Stat(intent.Path); err != nil {
					t.Errorf("File should exist at %q: %v", intent.Path, err)
				}
			},
		},
		{
			name: "minimal creation",
			opts: CreateOptions{
				Title:     "Minimal",
				Timestamp: time.Date(2026, 1, 19, 16, 0, 0, 0, time.UTC),
			},
			wantErr: false,
			checks: func(t *testing.T, intent *Intent) {
				if intent.Title != "Minimal" {
					t.Errorf("Title = %q, want %q", intent.Title, "Minimal")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := svc.CreateDirect(ctx, tt.opts)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CreateDirect() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if tt.checks != nil {
				tt.checks(t, intent)
			}
		})
	}
}

func TestIntentService_CreateDirect_DuplicateID(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	opts := CreateOptions{
		Title:     "Duplicate Test",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	}

	// First creation should succeed
	_, err := svc.CreateDirect(ctx, opts)
	if err != nil {
		t.Fatalf("First CreateDirect() error = %v", err)
	}

	// Second creation with same timestamp should fail
	_, err = svc.CreateDirect(ctx, opts)
	if err == nil {
		t.Fatal("Second CreateDirect() should fail with duplicate ID")
	}
	if !errors.Is(err, ErrFileExists) {
		t.Errorf("Expected ErrFileExists, got %v", err)
	}
}

func TestIntentService_CreateDirect_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := svc.CreateDirect(ctx, CreateOptions{Title: "Test"})
	if err == nil {
		t.Fatal("CreateDirect() should fail with cancelled context")
	}
}

func TestIntentService_CreateWithEditor(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Mock editor that modifies the file
	mockEditor := func(ctx context.Context, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Just modify the content slightly to indicate editing happened
		modified := strings.Replace(string(content), "## Description", "## Description\n\nEdited content.", 1)
		return os.WriteFile(path, []byte(modified), 0644)
	}

	intent, err := svc.CreateWithEditor(ctx, CreateOptions{
		Title:     "Edited Intent",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 1, 19, 17, 0, 0, 0, time.UTC),
	}, mockEditor)

	if err != nil {
		t.Fatalf("CreateWithEditor() error = %v", err)
	}
	if intent == nil {
		t.Fatal("CreateWithEditor() returned nil intent")
	}
	if !strings.Contains(intent.Content, "Edited content") {
		t.Error("Content should contain edited text")
	}
}

func TestIntentService_CreateWithEditor_Cancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Mock editor that doesn't change the content
	noOpEditor := func(ctx context.Context, path string) error {
		return nil // Don't modify the file
	}

	_, err := svc.CreateWithEditor(ctx, CreateOptions{
		Title:     "Cancelled Intent",
		Timestamp: time.Date(2026, 1, 19, 18, 0, 0, 0, time.UTC),
	}, noOpEditor)

	if !errors.Is(err, ErrCancelled) {
		t.Errorf("Expected ErrCancelled, got %v", err)
	}
}

func TestIntentService_Find(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create test intents
	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Findable Intent",
		Timestamp: time.Date(2026, 1, 19, 15, 34, 12, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	tests := []struct {
		name    string
		id      string
		wantErr bool
	}{
		{
			name:    "exact match",
			id:      created.ID,
			wantErr: false,
		},
		{
			name:    "partial match (timestamp)",
			id:      "153412",
			wantErr: false,
		},
		{
			name:    "partial match (slug)",
			id:      "findable",
			wantErr: false,
		},
		{
			name:    "not found",
			id:      "nonexistent-id",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intent, err := svc.Find(ctx, tt.id)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Find() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && intent.ID != created.ID {
				t.Errorf("Find() ID = %q, want %q", intent.ID, created.ID)
			}
		})
	}
}

func TestIntentService_Find_AcrossStatuses(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create an intent and move it to active
	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Status Test",
		Timestamp: time.Date(2026, 1, 19, 19, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	moved, err := svc.Move(ctx, created.ID, StatusActive)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	// Should still find it
	found, err := svc.Find(ctx, created.ID)
	if err != nil {
		t.Fatalf("Find() error = %v", err)
	}
	if found.Status != StatusActive {
		t.Errorf("Status = %q, want %q", found.Status, StatusActive)
	}
	if found.Path != moved.Path {
		t.Errorf("Path = %q, want %q", found.Path, moved.Path)
	}
}

func TestIntentService_Get(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Get Test",
		Timestamp: time.Date(2026, 1, 19, 20, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Exact ID should work
	intent, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if intent.ID != created.ID {
		t.Errorf("ID = %q, want %q", intent.ID, created.ID)
	}

	// Partial ID should NOT work with Get (only with Find)
	_, err = svc.Get(ctx, "get-test")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() with partial ID should return ErrNotFound, got %v", err)
	}
}

func TestIntentService_List(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create multiple intents
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "List Test 1",
		Type:      TypeFeature,
		Concept:   "project-a",
		Timestamp: time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	intent2, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "List Test 2",
		Type:      TypeBug,
		Concept:   "project-b",
		Timestamp: time.Date(2026, 1, 19, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Move one to active
	_, err = svc.Move(ctx, intent2.ID, StatusActive)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	tests := []struct {
		name      string
		opts      *ListOptions
		wantCount int
	}{
		{
			name:      "list all",
			opts:      nil,
			wantCount: 2,
		},
		{
			name:      "filter by inbox status",
			opts:      &ListOptions{Status: statusPtr(StatusInbox)},
			wantCount: 1,
		},
		{
			name:      "filter by active status",
			opts:      &ListOptions{Status: statusPtr(StatusActive)},
			wantCount: 1,
		},
		{
			name:      "filter by type",
			opts:      &ListOptions{Type: typePtr(TypeFeature)},
			wantCount: 1,
		},
		{
			name:      "filter by concept",
			opts:      &ListOptions{Concept: "project-a"},
			wantCount: 1,
		},
		{
			name:      "no matches",
			opts:      &ListOptions{Concept: "nonexistent"},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			intents, err := svc.List(ctx, tt.opts)
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(intents) != tt.wantCount {
				t.Errorf("List() count = %d, want %d", len(intents), tt.wantCount)
			}
		})
	}
}

func TestIntentService_List_Sorting(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create intents with different times and priorities
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "AAA First",
		Timestamp: time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "ZZZ Last",
		Timestamp: time.Date(2026, 1, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Default sort (newest first)
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(intents) != 2 {
		t.Fatalf("List() count = %d, want 2", len(intents))
	}
	if intents[0].Title != "ZZZ Last" {
		t.Errorf("Default sort should show newest first, got %q", intents[0].Title)
	}

	// Sort by title ascending
	intents, err = svc.List(ctx, &ListOptions{SortBy: "title", SortDesc: false})
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if intents[0].Title != "AAA First" {
		t.Errorf("Title sort ascending should show AAA first, got %q", intents[0].Title)
	}
}

func TestIntentService_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Delete Test",
		Timestamp: time.Date(2026, 1, 19, 21, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(created.Path); err != nil {
		t.Fatalf("File should exist before delete: %v", err)
	}

	// Delete
	if err := svc.Delete(ctx, created.ID); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(created.Path); !os.IsNotExist(err) {
		t.Error("File should not exist after delete")
	}

	// Finding should fail
	_, err = svc.Find(ctx, created.ID)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Find() after delete should return ErrNotFound, got %v", err)
	}
}

func TestIntentService_Move(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Move Test",
		Timestamp: time.Date(2026, 1, 19, 22, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	tests := []struct {
		name      string
		newStatus Status
	}{
		{name: "to active", newStatus: StatusActive},
		{name: "to ready", newStatus: StatusReady},
		{name: "to done", newStatus: StatusDone},
	}

	currentID := created.ID
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			moved, err := svc.Move(ctx, currentID, tt.newStatus)
			if err != nil {
				t.Fatalf("Move() error = %v", err)
			}
			if moved.Status != tt.newStatus {
				t.Errorf("Status = %q, want %q", moved.Status, tt.newStatus)
			}
			if !strings.Contains(moved.Path, string(tt.newStatus)) {
				t.Errorf("Path should contain %q, got %q", tt.newStatus, moved.Path)
			}
			// Verify file exists in new location
			if _, err := os.Stat(moved.Path); err != nil {
				t.Errorf("File should exist at new path: %v", err)
			}
		})
	}
}

func TestIntentService_Move_SameStatus(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Same Status",
		Timestamp: time.Date(2026, 1, 19, 23, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Moving to same status should be a no-op
	moved, err := svc.Move(ctx, created.ID, StatusInbox)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if moved.Path != created.Path {
		t.Errorf("Path changed when moving to same status")
	}
}

func TestIntentService_Archive(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Archive Test",
		Timestamp: time.Date(2026, 1, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	archived, err := svc.Archive(ctx, created.ID)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}
	if archived.Status != StatusArchived {
		t.Errorf("Status = %q, want %q", archived.Status, StatusArchived)
	}
	if !strings.Contains(archived.Path, "archived") {
		t.Errorf("Path should contain 'archived', got %q", archived.Path)
	}
}

func TestIntentService_Save(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Save Test",
		Timestamp: time.Date(2026, 1, 20, 1, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Modify and save
	created.Title = "Modified Title"
	created.Priority = PriorityHigh
	if err := svc.Save(ctx, created); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// Reload and verify
	reloaded, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if reloaded.Title != "Modified Title" {
		t.Errorf("Title = %q, want %q", reloaded.Title, "Modified Title")
	}
	if reloaded.Priority != PriorityHigh {
		t.Errorf("Priority = %q, want %q", reloaded.Priority, PriorityHigh)
	}
}

func TestIntentService_Edit(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Edit Test",
		Type:      TypeIdea,
		Timestamp: time.Date(2026, 1, 20, 2, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Mock editor that changes the type
	mockEditor := func(ctx context.Context, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		modified := strings.Replace(string(content), "type: idea", "type: feature", 1)
		return os.WriteFile(path, []byte(modified), 0644)
	}

	edited, err := svc.Edit(ctx, created.ID, mockEditor)
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if edited.Type != TypeFeature {
		t.Errorf("Type = %q, want %q", edited.Type, TypeFeature)
	}
}

func TestIntentService_Edit_StatusChange(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Edit Status Test",
		Timestamp: time.Date(2026, 1, 20, 3, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Mock editor that changes the status
	mockEditor := func(ctx context.Context, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		modified := strings.Replace(string(content), "status: inbox", "status: active", 1)
		return os.WriteFile(path, []byte(modified), 0644)
	}

	edited, err := svc.Edit(ctx, created.ID, mockEditor)
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if edited.Status != StatusActive {
		t.Errorf("Status = %q, want %q", edited.Status, StatusActive)
	}
	if !strings.Contains(edited.Path, "active") {
		t.Errorf("Path should contain 'active', got %q", edited.Path)
	}
	// Old file should not exist
	oldPath := filepath.Join(svc.intentsDir, "inbox", created.ID+".md")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("Old file should not exist after status change")
	}
}

// Helper functions for test options
func statusPtr(s Status) *Status {
	return &s
}

func typePtr(t Type) *Type {
	return &t
}

func TestMoveFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	content := "test content"
	if err := os.WriteFile(srcPath, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Move to destination
	dstPath := filepath.Join(tmpDir, "subdir", "dest.txt")
	if err := os.MkdirAll(filepath.Dir(dstPath), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	if err := moveFile(srcPath, dstPath); err != nil {
		t.Fatalf("moveFile() error = %v", err)
	}

	// Source should not exist
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}

	// Destination should exist with correct content
	data, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != content {
		t.Errorf("Content = %q, want %q", string(data), content)
	}
}

func TestIsCancelled(t *testing.T) {
	tests := []struct {
		name     string
		original string
		modified string
		want     bool
	}{
		{
			name:     "unchanged content",
			original: "test content",
			modified: "test content",
			want:     true,
		},
		{
			name:     "modified content",
			original: "test content",
			modified: "different content",
			want:     false,
		},
		{
			name:     "empty modified",
			original: "test content",
			modified: "",
			want:     true,
		},
		{
			name:     "whitespace only modified",
			original: "test content",
			modified: "   \n\t  ",
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isCancelled(tt.original, tt.modified)
			if got != tt.want {
				t.Errorf("isCancelled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPriorityRank(t *testing.T) {
	tests := []struct {
		priority Priority
		want     int
	}{
		{PriorityHigh, 3},
		{PriorityMedium, 2},
		{PriorityLow, 1},
		{"", 0},
		{"invalid", 0},
	}

	for _, tt := range tests {
		t.Run(string(tt.priority), func(t *testing.T) {
			got := priorityRank(tt.priority)
			if got != tt.want {
				t.Errorf("priorityRank(%q) = %d, want %d", tt.priority, got, tt.want)
			}
		})
	}
}

// Additional tests for better coverage

func TestIntentService_Save_NoPath(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	intent := &Intent{
		ID:    "test-id",
		Title: "Test",
		Path:  "", // No path set
	}

	err := svc.Save(ctx, intent)
	if err == nil {
		t.Fatal("Save() should fail when intent has no path")
	}
	if !errors.Is(err, ErrInvalidPath) {
		t.Errorf("Expected ErrInvalidPath, got %v", err)
	}
}

func TestIntentService_Save_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	intent := &Intent{
		ID:    "test-id",
		Title: "Test",
		Path:  filepath.Join(tmpDir, "test.md"),
	}

	err := svc.Save(ctx, intent)
	if err == nil {
		t.Fatal("Save() should fail with cancelled context")
	}
}

func TestIntentService_Delete_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	err := svc.Delete(ctx, "nonexistent-id")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete() non-existent should return ErrNotFound, got %v", err)
	}
}

func TestIntentService_Delete_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := svc.Delete(ctx, "any-id")
	if err == nil {
		t.Fatal("Delete() should fail with cancelled context")
	}
}

func TestIntentService_Move_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	_, err := svc.Move(ctx, "nonexistent-id", StatusActive)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Move() non-existent should return ErrNotFound, got %v", err)
	}
}

func TestIntentService_Move_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Move(ctx, "any-id", StatusActive)
	if err == nil {
		t.Fatal("Move() should fail with cancelled context")
	}
}

func TestIntentService_List_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.List(ctx, nil)
	if err == nil {
		t.Fatal("List() should fail with cancelled context")
	}
}

func TestIntentService_Find_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Find(ctx, "any-id")
	if err == nil {
		t.Fatal("Find() should fail with cancelled context")
	}
}

func TestIntentService_Get_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Get(ctx, "any-id")
	if err == nil {
		t.Fatal("Get() should fail with cancelled context")
	}
}

func TestIntentService_Edit_NotFound(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	_, err := svc.Edit(ctx, "nonexistent-id", nil)
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Edit() non-existent should return ErrNotFound, got %v", err)
	}
}

func TestIntentService_Edit_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Edit(ctx, "any-id", nil)
	if err == nil {
		t.Fatal("Edit() should fail with cancelled context")
	}
}

func TestIntentService_Edit_EditorError(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Editor Error Test",
		Timestamp: time.Date(2026, 1, 20, 4, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Mock editor that returns an error
	errorEditor := func(ctx context.Context, path string) error {
		return errors.New("editor failed")
	}

	_, err = svc.Edit(ctx, created.ID, errorEditor)
	if err == nil {
		t.Fatal("Edit() should fail when editor returns error")
	}
}

func TestIntentService_Edit_ValidationError(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Validation Error Test",
		Timestamp: time.Date(2026, 1, 20, 5, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Mock editor that breaks validation (removes title)
	breakingEditor := func(ctx context.Context, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		modified := strings.Replace(string(content), "title: Validation Error Test", "title:", 1)
		return os.WriteFile(path, []byte(modified), 0644)
	}

	_, err = svc.Edit(ctx, created.ID, breakingEditor)
	if err == nil {
		t.Fatal("Edit() should fail when validation fails")
	}
}

func TestIntentService_List_AllSortOptions(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create intents with different data for sorting
	intent1, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "First Intent",
		Timestamp: time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Modify first intent's UpdatedAt by saving
	intent1.UpdatedAt = time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC)
	if err := svc.Save(ctx, intent1); err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "Second Intent",
		Timestamp: time.Date(2026, 1, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Test all sort options
	sortOptions := []string{"created", "updated", "title", "priority", "invalid"}
	for _, sortBy := range sortOptions {
		t.Run("sort_by_"+sortBy, func(t *testing.T) {
			intents, err := svc.List(ctx, &ListOptions{SortBy: sortBy})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(intents) != 2 {
				t.Errorf("List() count = %d, want 2", len(intents))
			}
		})

		t.Run("sort_by_"+sortBy+"_desc", func(t *testing.T) {
			intents, err := svc.List(ctx, &ListOptions{SortBy: sortBy, SortDesc: true})
			if err != nil {
				t.Fatalf("List() error = %v", err)
			}
			if len(intents) != 2 {
				t.Errorf("List() count = %d, want 2", len(intents))
			}
		})
	}
}

func TestIntentService_CreateWithEditor_EditorError(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	errorEditor := func(ctx context.Context, path string) error {
		return errors.New("editor failed to open")
	}

	_, err := svc.CreateWithEditor(ctx, CreateOptions{
		Title:     "Editor Error",
		Timestamp: time.Date(2026, 1, 20, 6, 0, 0, 0, time.UTC),
	}, errorEditor)

	if err == nil {
		t.Fatal("CreateWithEditor() should fail when editor returns error")
	}
}

func TestIntentService_CreateWithEditor_ValidationFails(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Editor that clears the title (breaks validation)
	breakingEditor := func(ctx context.Context, path string) error {
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		modified := strings.Replace(string(content), "title: Validation Fail", "title:", 1)
		modified = modified + "\n# Modified"
		return os.WriteFile(path, []byte(modified), 0644)
	}

	_, err := svc.CreateWithEditor(ctx, CreateOptions{
		Title:     "Validation Fail",
		Timestamp: time.Date(2026, 1, 20, 7, 0, 0, 0, time.UTC),
	}, breakingEditor)

	if err == nil {
		t.Fatal("CreateWithEditor() should fail when validation fails")
	}
}

func TestMoveFile_SourceNotExist(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "dest.txt")

	err := moveFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("moveFile() should fail when source doesn't exist")
	}
}

func TestIntentService_loadIntent_Error(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	// Non-existent file
	_, err := svc.loadIntent(filepath.Join(tmpDir, "nonexistent.md"))
	if err == nil {
		t.Fatal("loadIntent() should fail for non-existent file")
	}
}

func TestIntentService_getIntentPath(t *testing.T) {
	svc := NewIntentService("/campaign", "/campaign/intents")

	tests := []struct {
		status Status
		id     string
		want   string
	}{
		{StatusInbox, "test-id", "/campaign/intents/inbox/test-id.md"},
		{StatusActive, "active-test", "/campaign/intents/active/active-test.md"},
		{StatusKilled, "killed-test", "/campaign/intents/dungeon/killed/killed-test.md"},
	}

	for _, tt := range tests {
		got := svc.getIntentPath(tt.status, tt.id)
		if got != tt.want {
			t.Errorf("getIntentPath(%q, %q) = %q, want %q", tt.status, tt.id, got, tt.want)
		}
	}
}

func TestIntentService_EnsureDirectories_CreatesCanonicalLayout(t *testing.T) {
	campaignRoot := t.TempDir()
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))
	ctx := context.Background()

	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	for _, status := range AllStatuses() {
		dir := filepath.Join(svc.intentsDir, string(status))
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected status directory %s to exist: %v", dir, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
	}
}

func TestIntentService_EnsureDirectories_MigratesLegacyRootAndAudit(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))
	ctx := context.Background()

	if err := os.MkdirAll(legacyRoot, 0755); err != nil {
		t.Fatalf("failed to create legacy intent root: %v", err)
	}
	if err := os.WriteFile(filepath.Join(legacyRoot, "OBEY.md"), []byte("# legacy intent docs\n"), 0644); err != nil {
		t.Fatalf("failed to write legacy OBEY.md: %v", err)
	}
	inboxIntent := mustWriteIntentFile(t, filepath.Join(legacyRoot, "inbox", "20260316-legacy-inbox.md"), StatusInbox, "legacy-inbox")
	doneIntent := mustWriteIntentFile(t, filepath.Join(legacyRoot, "done", "20260316-legacy-done.md"), StatusDone, "legacy-done")
	if err := os.WriteFile(filepath.Join(legacyRoot, ".intents.jsonl"), []byte("{\"event\":\"create\"}\n"), 0644); err != nil {
		t.Fatalf("failed to write legacy audit log: %v", err)
	}

	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	inboxPath := filepath.Join(svc.intentsDir, string(StatusInbox), inboxIntent.ID+".md")
	if _, err := os.Stat(inboxPath); err != nil {
		t.Fatalf("expected migrated inbox intent at %s: %v", inboxPath, err)
	}

	donePath := filepath.Join(svc.intentsDir, string(StatusDone), doneIntent.ID+".md")
	if _, err := os.Stat(donePath); err != nil {
		t.Fatalf("expected migrated done intent at %s: %v", donePath, err)
	}

	migratedDone, err := svc.loadIntent(donePath)
	if err != nil {
		t.Fatalf("loadIntent() error = %v", err)
	}
	if migratedDone.Status != StatusDone {
		t.Fatalf("migrated done status = %q, want %q", migratedDone.Status, StatusDone)
	}

	auditPath := filepath.Join(svc.intentsDir, ".intents.jsonl")
	if _, err := os.Stat(auditPath); err != nil {
		t.Fatalf("expected migrated audit log at %s: %v", auditPath, err)
	}

	if _, err := os.Stat(filepath.Join(legacyRoot, "inbox", inboxIntent.ID+".md")); !os.IsNotExist(err) {
		t.Fatalf("legacy inbox intent should be removed, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyRoot, ".intents.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("legacy audit log should be removed, err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(legacyRoot, "OBEY.md")); !os.IsNotExist(err) {
		t.Fatalf("legacy intent scaffold docs should be removed, err = %v", err)
	}
	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("legacy intent root should be removed when empty, err = %v", err)
	}
}

func TestIntentService_EnsureDirectories_RemovesLegacyScaffoldOnlyRoot(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))
	ctx := context.Background()

	for _, relPath := range []string{
		"OBEY.md",
		filepath.Join("inbox", ".gitkeep"),
		filepath.Join("ready", ".gitkeep"),
		filepath.Join("active", ".gitkeep"),
		filepath.Join("dungeon", ".gitkeep"),
		filepath.Join("dungeon", ".crawl.yaml"),
	} {
		path := filepath.Join(legacyRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("legacy scaffold\n"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}

	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories() error = %v", err)
	}

	if _, err := os.Stat(legacyRoot); !os.IsNotExist(err) {
		t.Fatalf("legacy intent root should be removed when it only contains scaffold residue, err = %v", err)
	}
}

func TestIntentService_EnsureDirectories_RerunAfterPartialCanonicalSetup(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))
	ctx := context.Background()

	if err := os.MkdirAll(filepath.Join(svc.intentsDir, "inbox"), 0755); err != nil {
		t.Fatalf("failed to create canonical inbox dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(svc.intentsDir, "dungeon", "done"), 0755); err != nil {
		t.Fatalf("failed to create canonical dungeon dir: %v", err)
	}

	legacyIntent := mustWriteIntentFile(t, filepath.Join(legacyRoot, "inbox", "20260316-legacy-rerun.md"), StatusInbox, "legacy-rerun")

	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("first EnsureDirectories() error = %v", err)
	}
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("second EnsureDirectories() error = %v", err)
	}

	migratedPath := filepath.Join(svc.intentsDir, string(StatusInbox), legacyIntent.ID+".md")
	if _, err := os.Stat(migratedPath); err != nil {
		t.Fatalf("expected migrated intent at %s: %v", migratedPath, err)
	}
}

func TestIntentService_EnsureDirectories_ConflictWhenBothRootsContainIntentData(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))
	ctx := context.Background()

	mustWriteIntentFile(t, filepath.Join(legacyRoot, "inbox", "20260316-legacy-conflict.md"), StatusInbox, "legacy-conflict")
	mustWriteIntentFile(t, filepath.Join(svc.intentsDir, "inbox", "20260316-canonical-conflict.md"), StatusInbox, "canonical-conflict")

	err := svc.EnsureDirectories(ctx)
	if err == nil {
		t.Fatal("EnsureDirectories() should fail when both legacy and canonical roots contain intent data")
	}
	if !errors.Is(err, ErrIntentMigrationConflict) {
		t.Fatalf("EnsureDirectories() error = %v, want ErrIntentMigrationConflict", err)
	}
}

func TestIntentService_PlanLegacyIntentRootMigration(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))

	if err := os.MkdirAll(filepath.Join(svc.intentsDir, "inbox"), 0755); err != nil {
		t.Fatalf("failed to create canonical inbox dir: %v", err)
	}

	legacyIntent := mustWriteIntentFile(t, filepath.Join(legacyRoot, "inbox", "20260316-legacy-plan.md"), StatusInbox, "legacy-plan")
	if err := os.WriteFile(filepath.Join(legacyRoot, ".intents.jsonl"), []byte("{\"event\":\"create\"}\n"), 0644); err != nil {
		t.Fatalf("failed to write legacy audit log: %v", err)
	}

	moves, err := svc.PlanLegacyIntentRootMigration()
	if err != nil {
		t.Fatalf("PlanLegacyIntentRootMigration() error = %v", err)
	}

	if len(moves) != 2 {
		t.Fatalf("expected 2 planned moves, got %d: %#v", len(moves), moves)
	}

	want := map[string]string{
		filepath.Join(legacyRoot, "inbox", legacyIntent.ID+".md"): filepath.Join(svc.intentsDir, "inbox", legacyIntent.ID+".md"),
		filepath.Join(legacyRoot, ".intents.jsonl"):               filepath.Join(svc.intentsDir, ".intents.jsonl"),
	}
	for _, move := range moves {
		dst, ok := want[move.Source]
		if !ok {
			t.Fatalf("unexpected planned move: %s -> %s", move.Source, move.Dest)
		}
		if move.Dest != dst {
			t.Fatalf("planned move dest = %s, want %s", move.Dest, dst)
		}
		delete(want, move.Source)
	}
	if len(want) != 0 {
		t.Fatalf("missing planned moves: %#v", want)
	}
}

func TestIntentService_PlanLegacyIntentRootCleanup(t *testing.T) {
	campaignRoot := t.TempDir()
	legacyRoot := filepath.Join(campaignRoot, "workflow", "intents")
	svc := NewIntentService(campaignRoot, filepath.Join(campaignRoot, ".campaign", "intents"))

	for _, relPath := range []string{
		"OBEY.md",
		filepath.Join("inbox", ".gitkeep"),
		filepath.Join("dungeon", ".crawl.yaml"),
	} {
		path := filepath.Join(legacyRoot, relPath)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("failed to create %s: %v", path, err)
		}
		if err := os.WriteFile(path, []byte("legacy scaffold\n"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
	}

	cleanup, err := svc.PlanLegacyIntentRootCleanup()
	if err != nil {
		t.Fatalf("PlanLegacyIntentRootCleanup() error = %v", err)
	}

	want := map[string]bool{
		filepath.Join(legacyRoot, "OBEY.md"):                false,
		filepath.Join(legacyRoot, "inbox", ".gitkeep"):      false,
		filepath.Join(legacyRoot, "dungeon", ".crawl.yaml"): false,
	}
	for _, path := range cleanup {
		if _, ok := want[path]; ok {
			want[path] = true
		}
	}
	for path, found := range want {
		if !found {
			t.Fatalf("expected cleanup plan to include %s, got %#v", path, cleanup)
		}
	}
}

func TestIntentService_List_EmptyDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// List with no intents created - should return empty slice
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(intents) != 0 {
		t.Errorf("List() should return empty slice, got %d items", len(intents))
	}
}

func TestIntentService_List_SkipsNonMarkdownFiles(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create inbox directory with non-markdown files
	inboxDir := filepath.Join(tmpDir, "intents", "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create a .txt file (should be ignored)
	if err := os.WriteFile(filepath.Join(inboxDir, "notes.txt"), []byte("test"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create an actual intent
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Real Intent",
		Timestamp: time.Date(2026, 1, 20, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(intents) != 1 {
		t.Errorf("List() should return 1 intent (ignoring .txt), got %d", len(intents))
	}
}

func TestIntentService_List_SkipsMalformedFiles(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create inbox directory
	inboxDir := filepath.Join(tmpDir, "intents", "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	// Create a malformed .md file
	if err := os.WriteFile(filepath.Join(inboxDir, "malformed.md"), []byte("not valid frontmatter"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create a valid intent
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Valid Intent",
		Timestamp: time.Date(2026, 1, 20, 9, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// List should succeed and skip malformed file
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(intents) != 1 {
		t.Errorf("List() should return 1 intent (skipping malformed), got %d", len(intents))
	}
}

func TestIntentService_CreateDirect_UseDefaultTimestamp(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Don't provide timestamp - should use time.Now()
	intent, err := svc.CreateDirect(ctx, CreateOptions{
		Title: "Auto Timestamp",
		// Timestamp not set
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Verify a timestamp was generated (ID should contain today's date)
	today := time.Now().Format("20060102")
	if !strings.Contains(intent.ID, today) {
		t.Errorf("ID should contain today's date %q, got %q", today, intent.ID)
	}
}

func TestIntentService_CreateWithEditor_ContextCancelledBeforeEditor(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.CreateWithEditor(ctx, CreateOptions{Title: "Test"}, nil)
	if err == nil {
		t.Fatal("CreateWithEditor() should fail with cancelled context")
	}
}

func TestIntentService_Move_AllStatuses(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Test moving to killed status (archive)
	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Move All Statuses",
		Timestamp: time.Date(2026, 1, 20, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Move to killed
	moved, err := svc.Move(ctx, created.ID, StatusKilled)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}
	if moved.Status != StatusKilled {
		t.Errorf("Status = %q, want %q", moved.Status, StatusKilled)
	}
}

func TestIntentService_Edit_NilEditor(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Nil Editor Test",
		Timestamp: time.Date(2026, 1, 20, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Calling Edit with nil editor should just re-read the file
	edited, err := svc.Edit(ctx, created.ID, nil)
	if err != nil {
		t.Fatalf("Edit() error = %v", err)
	}
	if edited.ID != created.ID {
		t.Errorf("ID = %q, want %q", edited.ID, created.ID)
	}
}

func TestIntentService_Search(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create test intents
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Add dark mode feature",
		Type:      TypeFeature,
		Timestamp: time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "Fix login bug",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 1, 19, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "Research OAuth providers",
		Type:      TypeResearch,
		Timestamp: time.Date(2026, 1, 19, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	tests := []struct {
		name      string
		query     string
		wantCount int
	}{
		{"empty query returns all", "", 3},
		{"match title word", "dark", 1},
		{"match title partial", "log", 1},
		{"match multiple words", "fix", 1},
		{"no match", "nonexistent", 0},
		{"case insensitive", "DARK", 1},
		{"match type in title", "bug", 1},
		{"fuzzy match initials", "adm", 1}, // matches "Add dark mode feature"
		{"fuzzy match abbreviation", "oauth", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			results, err := svc.Search(ctx, tt.query)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if len(results) != tt.wantCount {
				t.Errorf("Search(%q) returned %d results, want %d", tt.query, len(results), tt.wantCount)
			}
		})
	}
}

func TestIntentService_Search_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := svc.Search(ctx, "test")
	if err == nil {
		t.Fatal("Search() should return error for cancelled context")
	}
}

func TestIntentService_Count(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Empty directories — all counts should be zero
	counts, total, err := svc.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if total != 0 {
		t.Errorf("Count() total = %d, want 0", total)
	}
	for _, sc := range counts {
		if sc.Count != 0 {
			t.Errorf("Count() %s = %d, want 0", sc.Status, sc.Count)
		}
	}

	// Create intents across multiple statuses
	i1, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Count Test 1",
		Timestamp: time.Date(2026, 2, 1, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}
	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "Count Test 2",
		Timestamp: time.Date(2026, 2, 1, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}
	_, err = svc.CreateDirect(ctx, CreateOptions{
		Title:     "Count Test 3",
		Timestamp: time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Move one to active
	_, err = svc.Move(ctx, i1.ID, StatusActive)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	counts, total, err = svc.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if total != 3 {
		t.Errorf("Count() total = %d, want 3", total)
	}

	// Build a map for easier assertions
	countMap := make(map[Status]int)
	for _, sc := range counts {
		countMap[sc.Status] = sc.Count
	}
	if countMap[StatusInbox] != 2 {
		t.Errorf("inbox count = %d, want 2", countMap[StatusInbox])
	}
	if countMap[StatusActive] != 1 {
		t.Errorf("active count = %d, want 1", countMap[StatusActive])
	}
	if countMap[StatusReady] != 0 {
		t.Errorf("ready count = %d, want 0", countMap[StatusReady])
	}
}

func TestIntentService_Count_SkipsNonMarkdown(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create inbox with a non-markdown file
	inboxDir := filepath.Join(tmpDir, "intents", "inbox")
	if err := os.MkdirAll(inboxDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(inboxDir, "README.txt"), []byte("not an intent"), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Create one real intent
	_, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Real Intent",
		Timestamp: time.Date(2026, 2, 1, 13, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	counts, total, err := svc.Count(ctx)
	if err != nil {
		t.Fatalf("Count() error = %v", err)
	}
	if total != 1 {
		t.Errorf("Count() total = %d, want 1 (should skip non-md files)", total)
	}

	countMap := make(map[Status]int)
	for _, sc := range counts {
		countMap[sc.Status] = sc.Count
	}
	if countMap[StatusInbox] != 1 {
		t.Errorf("inbox count = %d, want 1", countMap[StatusInbox])
	}
}

func TestIntentService_Count_ContextCancelled(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := svc.Count(ctx)
	if err == nil {
		t.Fatal("Count() should fail with cancelled context")
	}
}

func TestIntentService_List_DeduplicatesByID(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create an intent (lands in inbox/)
	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Duplicate Bug Test",
		Type:      TypeFeature,
		Timestamp: time.Date(2026, 2, 17, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Manually copy the file to dungeon/killed/ to simulate an orphan
	killedDir := filepath.Join(tmpDir, "intents", string(StatusKilled))
	if err := os.MkdirAll(killedDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	srcContent, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	orphanPath := filepath.Join(killedDir, created.ID+".md")
	if err := os.WriteFile(orphanPath, srcContent, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// List should return exactly 1 result, not 2
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(intents) != 1 {
		t.Errorf("List() count = %d, want 1 (dedup should remove orphan)", len(intents))
	}

	// The returned intent should be from inbox (higher priority)
	if len(intents) > 0 && intents[0].Status != StatusInbox {
		t.Errorf("Status = %q, want %q (inbox has higher priority)", intents[0].Status, StatusInbox)
	}
}

func TestIntentService_Move_CleansUpOrphans(t *testing.T) {
	tmpDir := t.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create an intent (lands in inbox/)
	created, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "Orphan Cleanup Test",
		Type:      TypeBug,
		Timestamp: time.Date(2026, 2, 17, 11, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	// Manually copy the file to dungeon/killed/ to simulate an orphan
	killedDir := filepath.Join(tmpDir, "intents", string(StatusKilled))
	if err := os.MkdirAll(killedDir, 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	srcContent, err := os.ReadFile(created.Path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	orphanPath := filepath.Join(killedDir, created.ID+".md")
	if err := os.WriteFile(orphanPath, srcContent, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Move to active — should clean up both inbox and killed copies
	moved, err := svc.Move(ctx, created.ID, StatusActive)
	if err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	// Verify file exists only in active/
	if _, err := os.Stat(moved.Path); err != nil {
		t.Errorf("File should exist at active path: %v", err)
	}

	// Verify inbox copy is gone
	inboxPath := svc.getIntentPath(StatusInbox, created.ID)
	if _, err := os.Stat(inboxPath); !os.IsNotExist(err) {
		t.Error("Inbox copy should not exist after Move()")
	}

	// Verify killed orphan is gone
	if _, err := os.Stat(orphanPath); !os.IsNotExist(err) {
		t.Error("Killed orphan copy should not exist after Move()")
	}
}

func BenchmarkIntentService_Search(b *testing.B) {
	tmpDir := b.TempDir()
	svc := NewIntentService(tmpDir, filepath.Join(tmpDir, "intents"))
	ctx := context.Background()

	// Create 100 intents for benchmark
	for i := 0; i < 100; i++ {
		_, err := svc.CreateDirect(ctx, CreateOptions{
			Title:     fmt.Sprintf("Intent number %d for testing search", i),
			Type:      TypeFeature,
			Timestamp: time.Date(2026, 1, 19, 10, 0, i, 0, time.UTC),
		})
		if err != nil {
			b.Fatalf("CreateDirect() error = %v", err)
		}
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := svc.Search(ctx, "testing")
		if err != nil {
			b.Fatalf("Search() error = %v", err)
		}
	}
}

func mustWriteIntentFile(t *testing.T, path string, status Status, slug string) *Intent {
	t.Helper()

	intent := &Intent{
		ID:        "20260316-" + slug,
		Title:     "Intent " + slug,
		Status:    status,
		Type:      TypeFeature,
		CreatedAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2026, 3, 16, 12, 0, 0, 0, time.UTC),
		Content:   "test intent\n",
	}

	data, err := SerializeIntent(intent)
	if err != nil {
		t.Fatalf("SerializeIntent() error = %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("failed to create parent dir for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}

	return intent
}
