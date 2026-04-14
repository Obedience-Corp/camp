package intent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupTestService creates a temp campaign with an intent service and a single
// test intent in inbox. Returns the service, intent ID, and intents directory.
func setupTestService(t *testing.T) (*IntentService, string, string) {
	t.Helper()

	root := t.TempDir()
	intentsDir := filepath.Join(root, ".campaign", "intents")
	svc := NewIntentService(root, intentsDir)
	if err := svc.EnsureDirectories(context.Background()); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	ts := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	opts := CreateOptions{
		Title:     "Test intent title",
		Type:      TypeIdea,
		Author:    "agent",
		Body:      "Original body content",
		Concept:   "projects/camp",
		Timestamp: ts,
	}
	result, err := svc.CreateDirect(context.Background(), opts)
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	return svc, result.ID, intentsDir
}

func TestUpdateDirect_Title(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	newTitle := "Updated title"
	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Title: &newTitle,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if updated.Title != newTitle {
		t.Fatalf("Title = %q, want %q", updated.Title, newTitle)
	}
	if len(changes) != 1 {
		t.Fatalf("changes count = %d, want 1", len(changes))
	}
	if changes[0].Field != "title" {
		t.Fatalf("change field = %q, want %q", changes[0].Field, "title")
	}
	if changes[0].Old != "Test intent title" {
		t.Fatalf("change old = %q, want %q", changes[0].Old, "Test intent title")
	}
	if changes[0].New != newTitle {
		t.Fatalf("change new = %q, want %q", changes[0].New, newTitle)
	}
}

func TestUpdateDirect_Body(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	newBody := "Completely new body"
	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Body: &newBody,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if updated.Content != newBody {
		t.Fatalf("Content = %q, want %q", updated.Content, newBody)
	}

	found := false
	for _, c := range changes {
		if c.Field == "body" {
			found = true
			if c.New != newBody {
				t.Fatalf("body change new = %q, want %q", c.New, newBody)
			}
		}
	}
	if !found {
		t.Fatal("expected body change in changes slice")
	}
}

func TestUpdateDirect_AppendBody(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	appendText := "\nAdditional note"
	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Append: &appendText,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if !strings.HasSuffix(updated.Content, appendText) {
		t.Fatalf("Content should end with appended text, got %q", updated.Content)
	}

	if len(changes) == 0 {
		t.Fatal("expected at least one change for append")
	}
}

func TestUpdateDirect_MultipleFields(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	newTitle := "Multi-field update"
	newType := TypeFeature
	newPriority := PriorityHigh
	newHorizon := HorizonNow
	newConcept := "projects/fest"
	newAuthor := "human"

	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Title:    &newTitle,
		Type:     &newType,
		Priority: &newPriority,
		Horizon:  &newHorizon,
		Concept:  &newConcept,
		Author:   &newAuthor,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if updated.Title != newTitle {
		t.Fatalf("Title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Type != newType {
		t.Fatalf("Type = %q, want %q", updated.Type, newType)
	}
	if updated.Priority != newPriority {
		t.Fatalf("Priority = %q, want %q", updated.Priority, newPriority)
	}
	if updated.Horizon != newHorizon {
		t.Fatalf("Horizon = %q, want %q", updated.Horizon, newHorizon)
	}
	if updated.Concept != newConcept {
		t.Fatalf("Concept = %q, want %q", updated.Concept, newConcept)
	}
	if updated.Author != newAuthor {
		t.Fatalf("Author = %q, want %q", updated.Author, newAuthor)
	}

	// Should have 6 changes
	if len(changes) != 6 {
		t.Fatalf("changes count = %d, want 6", len(changes))
	}
}

func TestUpdateDirect_StatusChange(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	newStatus := StatusReady
	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Status: &newStatus,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if updated.Status != StatusReady {
		t.Fatalf("Status = %q, want %q", updated.Status, StatusReady)
	}

	// Verify the file was moved to the ready directory
	if !strings.Contains(updated.Path, "/ready/") {
		t.Fatalf("Path should contain /ready/, got %q", updated.Path)
	}

	// Verify the file exists at the new location
	if _, err := os.Stat(updated.Path); err != nil {
		t.Fatalf("file should exist at new path: %v", err)
	}

	// Verify at least one change (status)
	found := false
	for _, c := range changes {
		if c.Field == "status" {
			found = true
			if c.Old != "inbox" || c.New != "ready" {
				t.Fatalf("status change: old=%q new=%q, want old=inbox new=ready", c.Old, c.New)
			}
		}
	}
	if !found {
		t.Fatal("expected status change in changes slice")
	}
}

func TestUpdateDirect_NoChanges(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	// Set title to the same value
	sameTitle := "Test intent title"
	updated, changes, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Title: &sameTitle,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if len(changes) != 0 {
		t.Fatalf("expected 0 changes for identical values, got %d", len(changes))
	}
	if updated == nil {
		t.Fatal("expected non-nil intent even with no changes")
	}
}

func TestUpdateDirect_NoFieldsSpecified(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{})
	if err == nil {
		t.Fatal("expected error when no update fields specified")
	}
}

func TestUpdateDirect_NotFound(t *testing.T) {
	svc, _, _ := setupTestService(t)
	ctx := context.Background()

	newTitle := "Won't work"
	_, _, err := svc.UpdateDirect(ctx, "nonexistent-id", UpdateOptions{
		Title: &newTitle,
	})
	if err == nil {
		t.Fatal("expected error for nonexistent intent")
	}
}

func TestUpdateDirect_InvalidTypeValidation(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	invalidType := Type("invalid-type")
	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Type: &invalidType,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid type")
	}
}

func TestUpdateDirect_InvalidPriorityValidation(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	invalidPriority := Priority("critical")
	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Priority: &invalidPriority,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid priority")
	}
}

func TestUpdateDirect_InvalidHorizonValidation(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	invalidHorizon := Horizon("yesterday")
	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Horizon: &invalidHorizon,
	})
	if err == nil {
		t.Fatal("expected validation error for invalid horizon")
	}
}

func TestUpdateDirect_ContextCancellation(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	newTitle := "Won't work"
	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Title: &newTitle,
	})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestUpdateDirect_AppendToEmptyBody(t *testing.T) {
	root := t.TempDir()
	intentsDir := filepath.Join(root, ".campaign", "intents")
	svc := NewIntentService(root, intentsDir)
	if err := svc.EnsureDirectories(context.Background()); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	ts := time.Date(2026, 4, 13, 10, 0, 0, 0, time.UTC)
	// Create intent with empty body
	result, err := svc.CreateDirect(context.Background(), CreateOptions{
		Title:     "Empty body intent",
		Type:      TypeIdea,
		Author:    "agent",
		Timestamp: ts,
	})
	if err != nil {
		t.Fatalf("CreateDirect: %v", err)
	}

	appendText := "First content"
	updated, _, err := svc.UpdateDirect(context.Background(), result.ID, UpdateOptions{
		Append: &appendText,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	if !strings.Contains(updated.Content, "First content") {
		t.Fatalf("Content should contain appended text, got %q", updated.Content)
	}
}

func TestUpdateDirect_Persistence(t *testing.T) {
	svc, id, _ := setupTestService(t)
	ctx := context.Background()

	newTitle := "Persisted title"
	newBody := "Persisted body"
	_, _, err := svc.UpdateDirect(ctx, id, UpdateOptions{
		Title: &newTitle,
		Body:  &newBody,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	// Re-read from disk
	reloaded, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatalf("Find after update: %v", err)
	}

	if reloaded.Title != newTitle {
		t.Fatalf("persisted Title = %q, want %q", reloaded.Title, newTitle)
	}
	if reloaded.Content != newBody+"\n" && reloaded.Content != newBody {
		// Body may have trailing newline from serialization
		if !strings.HasPrefix(reloaded.Content, newBody) {
			t.Fatalf("persisted Content = %q, want prefix %q", reloaded.Content, newBody)
		}
	}
}

func TestTruncateForAudit(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantLen  int
		wantFull bool
	}{
		{"short", "hello", 5, true},
		{"exact200", strings.Repeat("x", 200), 200, true},
		{"over200", strings.Repeat("x", 201), 203, false}, // 200 + "..."
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := truncateForAudit(tt.input)
			if len(result) != tt.wantLen {
				t.Fatalf("length = %d, want %d", len(result), tt.wantLen)
			}
			if tt.wantFull && result != tt.input {
				t.Fatalf("expected full string preserved")
			}
			if !tt.wantFull && !strings.HasSuffix(result, "...") {
				t.Fatal("expected truncated string to end with ...")
			}
		})
	}
}
