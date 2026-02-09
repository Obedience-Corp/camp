package gather

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/intent"
)

func setupTestDir(t *testing.T) (string, *intent.IntentService) {
	t.Helper()

	tmpDir := t.TempDir()

	// Create status directories
	for _, status := range []string{"inbox", "active", "ready", "dungeon/done", "dungeon/killed", "dungeon/archived"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, status), 0755); err != nil {
			t.Fatal(err)
		}
	}

	svc := intent.NewIntentService(tmpDir, tmpDir)
	return tmpDir, svc
}

func createTestIntent(t *testing.T, svc *intent.IntentService, title string, tags []string) *intent.Intent {
	t.Helper()

	i, err := svc.CreateDirect(context.Background(), intent.CreateOptions{
		Title: title,
	})
	if err != nil {
		t.Fatalf("creating test intent: %v", err)
	}

	// Add tags if provided
	if len(tags) > 0 {
		i.Tags = tags
		if err := svc.Save(context.Background(), i); err != nil {
			t.Fatalf("saving test intent: %v", err)
		}
	}

	return i
}

func TestService_BuildIndex(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create test intents
	createTestIntent(t, svc, "Auth Feature", []string{"auth", "security"})
	createTestIntent(t, svc, "Login Bug", []string{"auth", "bug"})
	createTestIntent(t, svc, "Navigation", []string{"ui"})

	// Create gather service and build index
	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatalf("BuildIndex() error = %v", err)
	}

	// Check index size
	if gatherSvc.IndexSize() != 3 {
		t.Errorf("IndexSize() = %d, want 3", gatherSvc.IndexSize())
	}
}

func TestService_FindByTag(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create test intents
	createTestIntent(t, svc, "Auth Feature", []string{"auth", "security"})
	createTestIntent(t, svc, "Login Bug", []string{"auth", "bug"})
	createTestIntent(t, svc, "Navigation", []string{"ui"})

	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Find by auth tag
	authIntents, err := gatherSvc.FindByTag(context.Background(), "auth")
	if err != nil {
		t.Fatalf("FindByTag() error = %v", err)
	}

	if len(authIntents) != 2 {
		t.Errorf("FindByTag('auth') returned %d results, want 2", len(authIntents))
	}

	// Find by ui tag
	uiIntents, err := gatherSvc.FindByTag(context.Background(), "ui")
	if err != nil {
		t.Fatalf("FindByTag() error = %v", err)
	}

	if len(uiIntents) != 1 {
		t.Errorf("FindByTag('ui') returned %d results, want 1", len(uiIntents))
	}

	// Find by nonexistent tag
	noIntents, err := gatherSvc.FindByTag(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("FindByTag() error = %v", err)
	}

	if len(noIntents) != 0 {
		t.Errorf("FindByTag('nonexistent') returned %d results, want 0", len(noIntents))
	}
}

func TestService_GetAllTags(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	createTestIntent(t, svc, "Auth Feature", []string{"auth", "security"})
	createTestIntent(t, svc, "Login Bug", []string{"auth", "bug"})

	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatal(err)
	}

	tags := gatherSvc.GetAllTags()

	// Should have auth, security, bug
	if len(tags) < 3 {
		t.Errorf("GetAllTags() returned %d tags, want at least 3", len(tags))
	}
}

func TestService_TagCounts(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	createTestIntent(t, svc, "Auth Feature", []string{"auth", "security"})
	createTestIntent(t, svc, "Login Bug", []string{"auth", "bug"})

	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatal(err)
	}

	counts := gatherSvc.TagCounts()

	if counts["auth"] != 2 {
		t.Errorf("TagCounts()['auth'] = %d, want 2", counts["auth"])
	}
	if counts["security"] != 1 {
		t.Errorf("TagCounts()['security'] = %d, want 1", counts["security"])
	}
}

func TestService_Gather(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create test intents
	i1 := createTestIntent(t, svc, "Auth Feature", []string{"auth"})
	i2 := createTestIntent(t, svc, "Login Bug", []string{"auth", "bug"})

	gatherSvc := NewService(svc, tmpDir)

	// Gather the intents
	result, err := gatherSvc.Gather(context.Background(), []string{i1.ID, i2.ID}, GatherOptions{
		Title:          "Unified Auth System",
		ArchiveSources: true,
	})
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	// Check result
	if result.Gathered == nil {
		t.Fatal("Gather() returned nil gathered intent")
	}
	if result.SourceCount != 2 {
		t.Errorf("SourceCount = %d, want 2", result.SourceCount)
	}
	if len(result.ArchivedPaths) != 2 {
		t.Errorf("ArchivedPaths length = %d, want 2", len(result.ArchivedPaths))
	}

	// Check gathered intent
	if result.Gathered.Title != "Unified Auth System" {
		t.Errorf("Title = %q, want %q", result.Gathered.Title, "Unified Auth System")
	}

	// Check GatheredFrom
	if len(result.Gathered.GatheredFrom) != 2 {
		t.Errorf("GatheredFrom length = %d, want 2", len(result.Gathered.GatheredFrom))
	}

	// Check source intents were archived
	for _, path := range result.ArchivedPaths {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Archived file does not exist: %s", path)
		}
	}
}

func TestService_Gather_NoArchive(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create test intents
	i1 := createTestIntent(t, svc, "Auth Feature", []string{"auth"})
	i2 := createTestIntent(t, svc, "Login Bug", []string{"auth"})

	gatherSvc := NewService(svc, tmpDir)

	// Gather without archiving
	result, err := gatherSvc.Gather(context.Background(), []string{i1.ID, i2.ID}, GatherOptions{
		Title:          "Combined Feature",
		ArchiveSources: false,
	})
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	if len(result.ArchivedPaths) != 0 {
		t.Errorf("ArchivedPaths should be empty when ArchiveSources=false, got %d", len(result.ArchivedPaths))
	}

	// Source intents should still be in inbox
	_, err = svc.Get(context.Background(), i1.ID)
	if err != nil {
		t.Errorf("Source intent %s should still exist: %v", i1.ID, err)
	}
}

func TestService_Gather_Errors(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	gatherSvc := NewService(svc, tmpDir)

	// Test: not enough intents
	_, err := gatherSvc.Gather(context.Background(), []string{"only-one"}, GatherOptions{
		Title: "Test",
	})
	if err == nil {
		t.Error("Gather should error with only 1 intent ID")
	}

	// Test: empty title
	_, err = gatherSvc.Gather(context.Background(), []string{"one", "two"}, GatherOptions{})
	if err == nil {
		t.Error("Gather should error without title")
	}

	// Test: nonexistent intent
	i1 := createTestIntent(t, svc, "Real Intent", nil)
	_, err = gatherSvc.Gather(context.Background(), []string{i1.ID, "nonexistent"}, GatherOptions{
		Title: "Test",
	})
	if err == nil {
		t.Error("Gather should error with nonexistent intent ID")
	}
}

func TestService_Gather_WithOverrides(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	i1 := createTestIntent(t, svc, "Feature A", nil)
	i2 := createTestIntent(t, svc, "Feature B", nil)

	gatherSvc := NewService(svc, tmpDir)

	result, err := gatherSvc.Gather(context.Background(), []string{i1.ID, i2.ID}, GatherOptions{
		Title:          "Combined Feature",
		Type:           intent.TypeFeature,
		Priority:       intent.PriorityHigh,
		Horizon:        intent.HorizonNow,
		Concept:        "projects/camp",
		ArchiveSources: false,
	})
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	if result.Gathered.Type != intent.TypeFeature {
		t.Errorf("Type = %q, want %q", result.Gathered.Type, intent.TypeFeature)
	}
	if result.Gathered.Priority != intent.PriorityHigh {
		t.Errorf("Priority = %q, want %q", result.Gathered.Priority, intent.PriorityHigh)
	}
	if result.Gathered.Horizon != intent.HorizonNow {
		t.Errorf("Horizon = %q, want %q", result.Gathered.Horizon, intent.HorizonNow)
	}
	if result.Gathered.Concept != "projects/camp" {
		t.Errorf("Concept = %q, want %q", result.Gathered.Concept, "projects/camp")
	}
}

func TestService_FindByTag_ExcludesFinalStates(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create intents with same tag
	createTestIntent(t, svc, "Auth Feature", []string{"auth"})
	createTestIntent(t, svc, "Auth Bug", []string{"auth"})
	doneIntent := createTestIntent(t, svc, "Auth Done", []string{"auth"})

	// Move one to done status
	_, err := svc.Move(context.Background(), doneIntent.ID, intent.StatusDone)
	if err != nil {
		t.Fatalf("moving intent to done: %v", err)
	}

	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatal(err)
	}

	results, err := gatherSvc.FindByTag(context.Background(), "auth")
	if err != nil {
		t.Fatalf("FindByTag() error = %v", err)
	}

	// Should only return 2 (inbox intents), not the done one
	if len(results) != 2 {
		t.Errorf("FindByTag() returned %d results, want 2 (excluding done)", len(results))
	}
	for _, r := range results {
		if r.Status.InDungeon() {
			t.Errorf("FindByTag() returned intent %q with dungeon status %s", r.ID, r.Status)
		}
	}
}

func TestService_FindSimilar_ExcludesFinalStates(t *testing.T) {
	tmpDir, svc := setupTestDir(t)

	// Create intents with similar content
	ref := createTestIntent(t, svc, "Authentication login system", []string{"auth"})
	createTestIntent(t, svc, "Authentication login feature", []string{"auth"})
	killedIntent := createTestIntent(t, svc, "Authentication login module", []string{"auth"})

	// Move one to killed status
	_, err := svc.Move(context.Background(), killedIntent.ID, intent.StatusKilled)
	if err != nil {
		t.Fatalf("moving intent to killed: %v", err)
	}

	gatherSvc := NewService(svc, tmpDir)
	if err := gatherSvc.BuildIndex(context.Background()); err != nil {
		t.Fatal(err)
	}

	results, err := gatherSvc.FindSimilar(context.Background(), ref.ID, 0.0)
	if err != nil {
		t.Fatalf("FindSimilar() error = %v", err)
	}

	// None of the results should be in a dungeon state
	for _, r := range results {
		if r.Intent.Status.InDungeon() {
			t.Errorf("FindSimilar() returned intent %q with dungeon status %s", r.Intent.ID, r.Intent.Status)
		}
	}
}

func TestStatus_InDungeon(t *testing.T) {
	tests := []struct {
		status intent.Status
		want   bool
	}{
		{intent.StatusInbox, false},
		{intent.StatusActive, false},
		{intent.StatusReady, false},
		{intent.StatusDone, true},
		{intent.StatusKilled, true},
		{intent.StatusArchived, true},
		{intent.StatusSomeday, true},
	}
	for _, tt := range tests {
		if got := tt.status.InDungeon(); got != tt.want {
			t.Errorf("Status(%q).InDungeon() = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestService_ContextCancellation(t *testing.T) {
	tmpDir, svc := setupTestDir(t)
	gatherSvc := NewService(svc, tmpDir)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Test FindByTag with cancelled context
	_, err := gatherSvc.FindByTag(ctx, "test")
	if err == nil {
		t.Error("FindByTag() should error with cancelled context")
	}

	// Test FindByHashtag with cancelled context
	_, err = gatherSvc.FindByHashtag(ctx, "test")
	if err == nil {
		t.Error("FindByHashtag() should error with cancelled context")
	}

	// Test FindSimilar with cancelled context
	_, err = gatherSvc.FindSimilar(ctx, "test", 0.1)
	if err == nil {
		t.Error("FindSimilar() should error with cancelled context")
	}

	// Test Gather with cancelled context
	_, err = gatherSvc.Gather(ctx, []string{"a", "b"}, GatherOptions{Title: "Test"})
	if err == nil {
		t.Error("Gather() should error with cancelled context")
	}
}
