package intent

import (
	"context"
	"errors"
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
				Project:   "test-project",
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
		Project:   "project-a",
		Timestamp: time.Date(2026, 1, 19, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateDirect() error = %v", err)
	}

	intent2, err := svc.CreateDirect(ctx, CreateOptions{
		Title:     "List Test 2",
		Type:      TypeBug,
		Project:   "project-b",
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
			name:      "filter by project",
			opts:      &ListOptions{Project: "project-a"},
			wantCount: 1,
		},
		{
			name:      "no matches",
			opts:      &ListOptions{Project: "nonexistent"},
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
	if archived.Status != StatusKilled {
		t.Errorf("Status = %q, want %q", archived.Status, StatusKilled)
	}
	if !strings.Contains(archived.Path, "killed") {
		t.Errorf("Path should contain 'killed', got %q", archived.Path)
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
		{StatusKilled, "killed-test", "/campaign/intents/killed/killed-test.md"},
	}

	for _, tt := range tests {
		got := svc.getIntentPath(tt.status, tt.id)
		if got != tt.want {
			t.Errorf("getIntentPath(%q, %q) = %q, want %q", tt.status, tt.id, got, tt.want)
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
