package intent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	intentcore "github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/intent/audit"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// setupPipelineTest creates a full campaign environment and returns everything
// needed for end-to-end agent pipeline testing.
func setupPipelineTest(t *testing.T) (*intentcore.IntentService, string, *config.CampaignConfig, string) {
	t.Helper()

	root := t.TempDir()
	cfg := &config.CampaignConfig{
		ID:        "pipeline-test",
		Name:      "pipeline-campaign",
		Type:      config.CampaignTypeProduct,
		CreatedAt: time.Now(),
	}
	ctx := context.Background()
	if err := config.SaveCampaignConfig(ctx, root, cfg); err != nil {
		t.Fatalf("SaveCampaignConfig: %v", err)
	}
	jumps := config.DefaultJumpsConfig()
	if err := config.SaveJumpsConfig(ctx, root, &jumps); err != nil {
		t.Fatalf("SaveJumpsConfig: %v", err)
	}

	resolver := paths.NewResolverFromConfig(root, cfg)
	svc := intentcore.NewIntentService(root, resolver.Intents())
	if err := svc.EnsureDirectories(ctx); err != nil {
		t.Fatalf("EnsureDirectories: %v", err)
	}

	return svc, resolver.Intents(), cfg, root
}

// TestAgentPipeline_CreateThenEdit tests the full agent lifecycle:
// 1. Create intent with body/concept/author via CLI flags
// 2. Programmatically edit title, body, type, priority
// 3. Verify audit trail contains both create and edit events
// 4. Verify the final state on disk
func TestAgentPipeline_CreateThenEdit(t *testing.T) {
	svc, intentsDir, cfg, root := setupPipelineTest(t)
	ctx := context.Background()

	// Step 1: Create intent (simulating: camp intent add "Fix login" --body "500 error" --concept projects/camp --author agent)
	createOpts := intentcore.CreateOptions{
		Title:   "Fix login page",
		Type:    intentcore.TypeBug,
		Author:  "agent",
		Body:    "The login page returns 500 on POST",
		Concept: "projects/camp",
	}
	err := runFastCapture(ctx, svc, intentsDir, cfg, root, true, createOpts)
	if err != nil {
		t.Fatalf("runFastCapture (create): %v", err)
	}

	// Find the created intent
	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(intents) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(intents))
	}
	created := intents[0]

	// Verify creation state
	if created.Title != "Fix login page" {
		t.Fatalf("Title = %q", created.Title)
	}
	if created.Type != intentcore.TypeBug {
		t.Fatalf("Type = %q", created.Type)
	}
	if created.Author != "agent" {
		t.Fatalf("Author = %q", created.Author)
	}
	if created.Concept != "projects/camp" {
		t.Fatalf("Concept = %q", created.Concept)
	}
	if !strings.Contains(created.Content, "500 on POST") {
		t.Fatalf("Content missing body text")
	}

	// Step 2: Programmatic edit (simulating: camp intent edit <id> --title "Fix login 500" --set-type feature --priority high --body "Updated description")
	newTitle := "Fix login 500 error"
	newType := intentcore.TypeFeature
	newPriority := intentcore.PriorityHigh
	newBody := "Updated: login page returns 500 on POST with session cookie"
	newAuthor := "lance"

	updated, changes, err := svc.UpdateDirect(ctx, created.ID, intentcore.UpdateOptions{
		Title:    &newTitle,
		Type:     &newType,
		Priority: &newPriority,
		Body:     &newBody,
		Author:   &newAuthor,
	})
	if err != nil {
		t.Fatalf("UpdateDirect: %v", err)
	}

	// Emit audit event (as the command handler would)
	auditChanges := make([]audit.FieldChange, len(changes))
	for i, c := range changes {
		auditChanges[i] = audit.FieldChange{
			Field: c.Field,
			Old:   c.Old,
			New:   c.New,
		}
	}
	if err := appendIntentAuditEvent(ctx, intentsDir, audit.Event{
		Type:    audit.EventEdit,
		ID:      updated.ID,
		Title:   updated.Title,
		Changes: auditChanges,
	}); err != nil {
		t.Fatalf("appendIntentAuditEvent: %v", err)
	}

	// Step 3: Verify final state
	if updated.Title != newTitle {
		t.Fatalf("Updated Title = %q, want %q", updated.Title, newTitle)
	}
	if updated.Type != newType {
		t.Fatalf("Updated Type = %q, want %q", updated.Type, newType)
	}
	if updated.Priority != newPriority {
		t.Fatalf("Updated Priority = %q, want %q", updated.Priority, newPriority)
	}
	if updated.Content != newBody {
		t.Fatalf("Updated Content = %q, want %q", updated.Content, newBody)
	}
	if updated.Author != newAuthor {
		t.Fatalf("Updated Author = %q, want %q", updated.Author, newAuthor)
	}

	// Verify changes recorded
	if len(changes) != 5 {
		t.Fatalf("changes count = %d, want 5", len(changes))
	}

	// Step 4: Verify audit trail
	auditPath := audit.FilePath(intentsDir)
	auditData, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("ReadFile(audit): %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(auditData)), "\n")
	if len(lines) < 2 {
		t.Fatalf("audit log should have at least 2 entries (create + edit), got %d", len(lines))
	}

	// Verify last audit entry is an edit with changes
	var lastEvent audit.Event
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &lastEvent); err != nil {
		t.Fatalf("Unmarshal last audit event: %v", err)
	}
	if lastEvent.Type != audit.EventEdit {
		t.Fatalf("last event type = %q, want %q", lastEvent.Type, audit.EventEdit)
	}
	if len(lastEvent.Changes) != 5 {
		t.Fatalf("last event changes = %d, want 5", len(lastEvent.Changes))
	}

	// Step 5: Re-read from disk to verify persistence
	reloaded, err := svc.Find(ctx, created.ID)
	if err != nil {
		t.Fatalf("Find after update: %v", err)
	}
	if reloaded.Title != newTitle {
		t.Fatalf("Reloaded Title = %q, want %q", reloaded.Title, newTitle)
	}
	if reloaded.Type != newType {
		t.Fatalf("Reloaded Type = %q", reloaded.Type)
	}
}

// TestAgentPipeline_CreateThenAppend tests creating then appending body content.
func TestAgentPipeline_CreateThenAppend(t *testing.T) {
	svc, intentsDir, cfg, root := setupPipelineTest(t)
	ctx := context.Background()

	// Create
	err := runFastCapture(ctx, svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Research task",
		Type:   intentcore.TypeResearch,
		Author: "agent",
		Body:   "Initial research notes",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	created := intents[0]

	// Append body
	appendText := "\n\n## Additional Findings\nFound a relevant paper on the topic."
	updated, _, err := svc.UpdateDirect(ctx, created.ID, intentcore.UpdateOptions{
		Append: &appendText,
	})
	if err != nil {
		t.Fatalf("UpdateDirect(append): %v", err)
	}

	if !strings.Contains(updated.Content, "Initial research notes") {
		t.Fatal("original body should be preserved")
	}
	if !strings.Contains(updated.Content, "Additional Findings") {
		t.Fatal("appended text should be present")
	}
}

// TestAgentPipeline_CreateThenStatusChange tests moving an intent through the lifecycle.
func TestAgentPipeline_CreateThenStatusChange(t *testing.T) {
	svc, intentsDir, cfg, root := setupPipelineTest(t)
	ctx := context.Background()

	// Create in inbox
	err := runFastCapture(ctx, svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Lifecycle test",
		Type:   intentcore.TypeIdea,
		Author: "agent",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	created := intents[0]
	if created.Status != intentcore.StatusInbox {
		t.Fatalf("initial status = %q, want inbox", created.Status)
	}

	// Move to ready via programmatic edit
	readyStatus := intentcore.StatusReady
	updated, changes, err := svc.UpdateDirect(ctx, created.ID, intentcore.UpdateOptions{
		Status: &readyStatus,
	})
	if err != nil {
		t.Fatalf("UpdateDirect(ready): %v", err)
	}
	if updated.Status != intentcore.StatusReady {
		t.Fatalf("status after edit = %q, want ready", updated.Status)
	}
	if !strings.Contains(updated.Path, "/ready/") {
		t.Fatalf("path should contain /ready/, got %q", updated.Path)
	}

	// Verify the change was recorded
	found := false
	for _, c := range changes {
		if c.Field == "status" && c.Old == "inbox" && c.New == "ready" {
			found = true
		}
	}
	if !found {
		t.Fatal("status change not recorded in changes")
	}

	// Verify old file is gone
	oldPath := filepath.Join(intentsDir, "inbox", created.ID+".md")
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Fatal("old file in inbox should be removed")
	}
}

// TestAgentPipeline_MultipleEdits verifies successive edits accumulate correctly.
func TestAgentPipeline_MultipleEdits(t *testing.T) {
	svc, intentsDir, cfg, root := setupPipelineTest(t)
	ctx := context.Background()

	err := runFastCapture(ctx, svc, intentsDir, cfg, root, true, intentcore.CreateOptions{
		Title:  "Multi-edit intent",
		Type:   intentcore.TypeIdea,
		Author: "agent",
	})
	if err != nil {
		t.Fatalf("runFastCapture: %v", err)
	}

	intents, err := svc.List(ctx, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	id := intents[0].ID

	// Edit 1: set type
	featureType := intentcore.TypeFeature
	_, _, err = svc.UpdateDirect(ctx, id, intentcore.UpdateOptions{Type: &featureType})
	if err != nil {
		t.Fatalf("edit 1: %v", err)
	}

	// Edit 2: set priority
	highPriority := intentcore.PriorityHigh
	_, _, err = svc.UpdateDirect(ctx, id, intentcore.UpdateOptions{Priority: &highPriority})
	if err != nil {
		t.Fatalf("edit 2: %v", err)
	}

	// Edit 3: set body
	body := "Detailed description"
	_, _, err = svc.UpdateDirect(ctx, id, intentcore.UpdateOptions{Body: &body})
	if err != nil {
		t.Fatalf("edit 3: %v", err)
	}

	// Verify final state
	final, err := svc.Find(ctx, id)
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if final.Type != intentcore.TypeFeature {
		t.Fatalf("Type = %q, want feature", final.Type)
	}
	if final.Priority != intentcore.PriorityHigh {
		t.Fatalf("Priority = %q, want high", final.Priority)
	}
	if !strings.Contains(final.Content, "Detailed description") {
		t.Fatalf("body not preserved, Content = %q", final.Content)
	}
}
