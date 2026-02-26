package dungeon

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/workflow"
)

func TestService_ListItems(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add test file
	testFile := filepath.Join(dungeonPath, "test-file.txt")
	if err := os.WriteFile(testFile, []byte("test"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	// Add test directory at dungeon root (should NOT appear - dirs are excluded as status dirs)
	testDir := filepath.Join(dungeonPath, "test-dir")
	if err := os.MkdirAll(testDir, 0755); err != nil {
		t.Fatalf("failed to create test dir: %v", err)
	}

	// List items
	items, err := svc.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems failed: %v", err)
	}

	// Should have 1 item (only files; directories at root are treated as status dirs)
	if len(items) != 1 {
		t.Errorf("expected 1 item, got %d", len(items))
	}

	if len(items) > 0 && items[0].Name != "test-file.txt" {
		t.Errorf("expected test-file.txt, got %s", items[0].Name)
	}
}

func TestService_ListStatusDirs(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon (creates completed, archived, someday)
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Add a custom status dir
	readyDir := filepath.Join(dungeonPath, "ready")
	if err := os.MkdirAll(readyDir, 0755); err != nil {
		t.Fatalf("failed to create ready dir: %v", err)
	}

	// Add items to some dirs
	if err := os.WriteFile(filepath.Join(dungeonPath, "completed", "item1.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dungeonPath, "completed", "item2.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create item: %v", err)
	}

	dirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		t.Fatalf("ListStatusDirs failed: %v", err)
	}

	// Should have 4 dirs: archived, completed, ready, someday (sorted alphabetically)
	if len(dirs) != 4 {
		t.Fatalf("expected 4 status dirs, got %d", len(dirs))
	}

	// Check alphabetical order
	expected := []string{"archived", "completed", "ready", "someday"}
	for i, d := range dirs {
		if d.Name != expected[i] {
			t.Errorf("dir[%d] = %s, want %s", i, d.Name, expected[i])
		}
	}

	// Check completed has 2 items (+ .gitkeep = 3 entries, but .gitkeep excluded = 2)
	for _, d := range dirs {
		if d.Name == "completed" && d.ItemCount != 2 {
			t.Errorf("completed should have 2 items, got %d", d.ItemCount)
		}
		if d.Name == "ready" && d.ItemCount != 0 {
			t.Errorf("ready should have 0 items, got %d", d.ItemCount)
		}
	}
}

func TestService_ListStatusDirs_Empty(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create dungeon with no subdirs
	dungeonPath := filepath.Join(tmpDir, "dungeon")
	if err := os.MkdirAll(dungeonPath, 0755); err != nil {
		t.Fatalf("failed to create dungeon dir: %v", err)
	}

	svc := NewService(tmpDir, dungeonPath)

	dirs, err := svc.ListStatusDirs(ctx)
	if err != nil {
		t.Fatalf("ListStatusDirs failed: %v", err)
	}

	if len(dirs) != 0 {
		t.Errorf("expected 0 status dirs, got %d", len(dirs))
	}
}

func TestService_AppendCrawlLog(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	// Init dungeon
	_, err = svc.Init(ctx, InitOptions{})
	if err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Append entry
	entry := CrawlEntry{
		Item:     "test-item",
		Decision: DecisionKeep,
	}

	if err := svc.AppendCrawlLog(ctx, entry); err != nil {
		t.Fatalf("AppendCrawlLog failed: %v", err)
	}

	// Verify file exists and has content
	logPath := filepath.Join(dungeonPath, "crawl.jsonl")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read crawl log: %v", err)
	}

	if len(data) == 0 {
		t.Error("crawl log should not be empty")
	}

	// Append another entry with MoveDecision
	entry2 := CrawlEntry{
		Item:     "test-item-2",
		Decision: MoveDecision("archived"),
	}

	if err := svc.AppendCrawlLog(ctx, entry2); err != nil {
		t.Fatalf("second AppendCrawlLog failed: %v", err)
	}

	// Verify two lines
	data, err = os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read crawl log: %v", err)
	}

	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}

	if lines != 2 {
		t.Errorf("expected 2 lines in crawl log, got %d", lines)
	}
}

func TestListParentItems_ExcludesOBEYManagedDirs(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a managed directory (has OBEY.md) — should be excluded
	managedDir := filepath.Join(tmpDir, "code_reviews")
	if err := os.MkdirAll(managedDir, 0755); err != nil {
		t.Fatalf("failed to create managed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managedDir, "OBEY.md"), []byte("# Managed"), 0644); err != nil {
		t.Fatalf("failed to create OBEY.md: %v", err)
	}

	// Create an unmanaged directory (no OBEY.md) — should be included
	unmanagedDir := filepath.Join(tmpDir, "misc-stuff")
	if err := os.MkdirAll(unmanagedDir, 0755); err != nil {
		t.Fatalf("failed to create unmanaged dir: %v", err)
	}

	// Create a regular file — should be included
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.txt"), []byte("notes"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	items, err := svc.ListParentItems(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListParentItems failed: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
	}

	if names["code_reviews"] {
		t.Error("managed directory with OBEY.md should be excluded")
	}
	if !names["misc-stuff"] {
		t.Error("unmanaged directory without OBEY.md should be included")
	}
	if !names["notes.txt"] {
		t.Error("regular file should be included")
	}
}

func TestListParentItems_ExcludesWorkflowSchemaDirs(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a .workflow.yaml in the parent directory
	schemaContent := `version: 1
type: status-workflow
name: test-workflow
directories:
  active:
    description: "Active items"
  ready:
    description: "Ready items"
  dungeon:
    description: "Archived"
    nested: true
    children:
      completed:
        description: "Done"
      archived:
        description: "Archived"
default_status: active
`
	schemaPath := filepath.Join(tmpDir, workflow.SchemaFileName)
	if err := os.WriteFile(schemaPath, []byte(schemaContent), 0644); err != nil {
		t.Fatalf("failed to write workflow schema: %v", err)
	}

	// Create the workflow-defined directories
	for _, dir := range []string{"active", "ready"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Create a non-workflow directory — should be included
	if err := os.MkdirAll(filepath.Join(tmpDir, "random-project"), 0755); err != nil {
		t.Fatalf("failed to create random-project dir: %v", err)
	}

	// Create a file — should be included
	if err := os.WriteFile(filepath.Join(tmpDir, "stale-doc.md"), []byte("old"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	items, err := svc.ListParentItems(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListParentItems failed: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
	}

	// Workflow-defined directories should be excluded
	if names["active"] {
		t.Error("workflow-defined directory 'active' should be excluded")
	}
	if names["ready"] {
		t.Error("workflow-defined directory 'ready' should be excluded")
	}

	// Non-workflow items should be included
	if !names["random-project"] {
		t.Error("non-workflow directory should be included")
	}
	if !names["stale-doc.md"] {
		t.Error("regular file should be included")
	}
}

func TestListParentItems_BothExclusionsCombined(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Workflow schema defines "active" dir
	schemaContent := `version: 1
type: status-workflow
name: test
directories:
  active:
    description: "Active"
default_status: active
`
	if err := os.WriteFile(filepath.Join(tmpDir, workflow.SchemaFileName), []byte(schemaContent), 0644); err != nil {
		t.Fatalf("failed to write schema: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "active"), 0755); err != nil {
		t.Fatalf("failed to create active dir: %v", err)
	}

	// OBEY.md-managed dir
	managed := filepath.Join(tmpDir, "pipelines")
	if err := os.MkdirAll(managed, 0755); err != nil {
		t.Fatalf("failed to create managed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, "OBEY.md"), []byte("# Pipes"), 0644); err != nil {
		t.Fatalf("failed to create OBEY.md: %v", err)
	}

	// Regular content that should survive
	if err := os.WriteFile(filepath.Join(tmpDir, "leftover.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "old-project"), 0755); err != nil {
		t.Fatalf("failed to create old-project dir: %v", err)
	}

	items, err := svc.ListParentItems(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListParentItems failed: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
	}

	if names["active"] {
		t.Error("workflow-defined 'active' should be excluded")
	}
	if names["pipelines"] {
		t.Error("OBEY.md-managed 'pipelines' should be excluded")
	}
	if !names["leftover.txt"] {
		t.Error("regular file should be included")
	}
	if !names["old-project"] {
		t.Error("unmanaged directory should be included")
	}
}

func TestListParentItems_ExcludesCrawlConfigDirs(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Create a .crawl.yaml in the dungeon with excludes
	crawlCfg := "excludes:\n  - templates\n  - scripts\n"
	if err := os.WriteFile(filepath.Join(dungeonPath, CrawlConfigFile), []byte(crawlCfg), 0644); err != nil {
		t.Fatalf("failed to write crawl config: %v", err)
	}

	// Create excluded directories — should not appear in triage
	for _, dir := range []string{"templates", "scripts"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatalf("failed to create dir %s: %v", dir, err)
		}
	}

	// Create a non-excluded directory — should be included
	if err := os.MkdirAll(filepath.Join(tmpDir, "my-review"), 0755); err != nil {
		t.Fatalf("failed to create my-review dir: %v", err)
	}

	// Create a regular file — should be included
	if err := os.WriteFile(filepath.Join(tmpDir, "notes.md"), []byte("notes"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	items, err := svc.ListParentItems(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListParentItems failed: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
	}

	// Crawl config-excluded directories should not appear
	if names["templates"] {
		t.Error("crawl config-excluded directory 'templates' should be excluded")
	}
	if names["scripts"] {
		t.Error("crawl config-excluded directory 'scripts' should be excluded")
	}

	// Non-excluded items should be included
	if !names["my-review"] {
		t.Error("non-excluded directory should be included")
	}
	if !names["notes.md"] {
		t.Error("regular file should be included")
	}
}

func TestListParentItems_AllExclusionLayersCombined(t *testing.T) {
	ctx := context.Background()

	tmpDir, err := os.MkdirTemp("", "dungeon-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	dungeonPath := filepath.Join(tmpDir, "dungeon")
	svc := NewService(tmpDir, dungeonPath)

	if _, err := svc.Init(ctx, InitOptions{}); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	// Layer 2: Workflow schema defines "active" dir
	schemaContent := `version: 1
type: status-workflow
name: test
directories:
  active:
    description: "Active"
default_status: active
`
	if err := os.WriteFile(filepath.Join(tmpDir, workflow.SchemaFileName), []byte(schemaContent), 0644); err != nil {
		t.Fatalf("failed to write schema: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "active"), 0755); err != nil {
		t.Fatalf("failed to create active dir: %v", err)
	}

	// Layer 3: OBEY.md-managed dir
	managed := filepath.Join(tmpDir, "pipelines")
	if err := os.MkdirAll(managed, 0755); err != nil {
		t.Fatalf("failed to create managed dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(managed, "OBEY.md"), []byte("# Pipes"), 0644); err != nil {
		t.Fatalf("failed to create OBEY.md: %v", err)
	}

	// Layer 4: Crawl config excludes
	crawlCfg := "excludes:\n  - templates\n"
	if err := os.WriteFile(filepath.Join(dungeonPath, CrawlConfigFile), []byte(crawlCfg), 0644); err != nil {
		t.Fatalf("failed to write crawl config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "templates"), 0755); err != nil {
		t.Fatalf("failed to create templates dir: %v", err)
	}

	// Regular content that should survive all layers
	if err := os.WriteFile(filepath.Join(tmpDir, "leftover.txt"), []byte("x"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmpDir, "old-review"), 0755); err != nil {
		t.Fatalf("failed to create old-review dir: %v", err)
	}

	items, err := svc.ListParentItems(ctx, tmpDir)
	if err != nil {
		t.Fatalf("ListParentItems failed: %v", err)
	}

	names := make(map[string]bool)
	for _, item := range items {
		names[item.Name] = true
	}

	// All exclusion layers should work together
	if names["active"] {
		t.Error("workflow-defined 'active' should be excluded (Layer 2)")
	}
	if names["pipelines"] {
		t.Error("OBEY.md-managed 'pipelines' should be excluded (Layer 3)")
	}
	if names["templates"] {
		t.Error("crawl config-excluded 'templates' should be excluded (Layer 4)")
	}

	// Regular content survives
	if !names["leftover.txt"] {
		t.Error("regular file should be included")
	}
	if !names["old-review"] {
		t.Error("unmanaged, non-excluded directory should be included")
	}
}
