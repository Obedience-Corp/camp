package workitem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func setupTestCampaign(t *testing.T) (string, *paths.Resolver) {
	t.Helper()
	root := t.TempDir()

	// Create standard campaign directories
	dirs := []string{
		".campaign/intents/inbox",
		".campaign/intents/active",
		".campaign/intents/ready",
		".campaign/intents/dungeon/done",
		"workflow/design",
		"workflow/design/dungeon",
		"workflow/explore",
		"workflow/explore/dungeon",
		"festivals/planning",
		"festivals/ready",
		"festivals/active",
		"festivals/chains",
		"festivals/ritual",
	}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(root, d), 0755); err != nil {
			t.Fatal(err)
		}
	}

	resolver := paths.NewResolver(root, config.DefaultCampaignPaths())
	return root, resolver
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestDiscoverIntents_ActiveOnly(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(root, ".campaign/intents/inbox/test-intent.md"),
		"---\nid: test-20260101\ntitle: Test Intent\nstatus: inbox\ncreated_at: 2026-01-01\ntype: feature\n---\n\n# Test Intent\n\nBody text here.")

	// Dungeon intent should NOT be discovered (we don't scan dungeon dirs)
	writeFile(t, filepath.Join(root, ".campaign/intents/dungeon/done/old.md"),
		"---\nid: old-20250101\ntitle: Old Done\nstatus: dungeon/done\ncreated_at: 2025-01-01\n---\n\n# Old")

	items, err := discoverIntents(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 intent, got %d", len(items))
	}
	if items[0].Title != "Test Intent" {
		t.Errorf("title = %q, want 'Test Intent'", items[0].Title)
	}
	if items[0].LifecycleStage != "inbox" {
		t.Errorf("stage = %q, want 'inbox'", items[0].LifecycleStage)
	}
	if items[0].WorkflowType != WorkflowTypeIntent {
		t.Errorf("type = %q, want 'intent'", items[0].WorkflowType)
	}
	if items[0].ItemKind != ItemKindFile {
		t.Errorf("kind = %q, want 'file'", items[0].ItemKind)
	}
}

func TestDiscoverIntents_SkipsGitkeep(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	writeFile(t, filepath.Join(root, ".campaign/intents/inbox/.gitkeep"), "")

	items, err := discoverIntents(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items (only .gitkeep), got %d", len(items))
	}
}

func TestDiscoverDesign_ExcludesDungeonAndHidden(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	// Valid design
	designDir := filepath.Join(root, "workflow/design/my-feature")
	os.MkdirAll(designDir, 0755)
	writeFile(t, filepath.Join(designDir, "README.md"), "# My Feature\n\nDesign doc.")

	// Hidden dir — should be excluded
	os.MkdirAll(filepath.Join(root, "workflow/design/.hidden"), 0755)

	// Dungeon — should be excluded (created in setupTestCampaign)

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 design, got %d", len(items))
	}
	if items[0].Title != "My Feature" {
		t.Errorf("title = %q, want 'My Feature' (from README heading)", items[0].Title)
	}
	if items[0].WorkflowType != WorkflowTypeDesign {
		t.Errorf("type = %q, want 'design'", items[0].WorkflowType)
	}
	if items[0].ItemKind != ItemKindDirectory {
		t.Errorf("kind = %q, want 'directory'", items[0].ItemKind)
	}
}

func TestDiscoverExplore_Works(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	exploreDir := filepath.Join(root, "workflow/explore/research-topic")
	os.MkdirAll(exploreDir, 0755)
	writeFile(t, filepath.Join(exploreDir, "notes.md"), "# Research Topic\n\nNotes.")

	items, err := discoverExplore(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 explore, got %d", len(items))
	}
	if items[0].WorkflowType != WorkflowTypeExplore {
		t.Errorf("type = %q, want 'explore'", items[0].WorkflowType)
	}
}

func TestDiscoverFestivals_OnlyPlanningReadyActive(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	// Active festival with fest.yaml
	festDir := filepath.Join(root, "festivals/active/test-fest-TF0001")
	os.MkdirAll(festDir, 0755)
	writeFile(t, filepath.Join(festDir, "fest.yaml"),
		"version: \"1.0\"\nmetadata:\n  id: TF0001\n  name: test-fest\n  festival_type: implementation\n  created_at: 2026-03-01T00:00:00Z\n")
	writeFile(t, filepath.Join(festDir, "FESTIVAL_GOAL.md"), "# Test Fest\n\nGoal description.")

	// Chain festival — should be excluded
	chainDir := filepath.Join(root, "festivals/chains/chained")
	os.MkdirAll(chainDir, 0755)

	// Ritual festival — should be excluded
	ritualDir := filepath.Join(root, "festivals/ritual/daily")
	os.MkdirAll(ritualDir, 0755)

	items, err := discoverFestivals(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 1 {
		t.Fatalf("expected 1 festival (chains/ritual excluded), got %d", len(items))
	}
	if items[0].Title != "test-fest (TF0001)" {
		t.Errorf("title = %q, want 'test-fest (TF0001)'", items[0].Title)
	}
	if items[0].LifecycleStage != "active" {
		t.Errorf("stage = %q, want 'active'", items[0].LifecycleStage)
	}
	if items[0].SourceID != "TF0001" {
		t.Errorf("source_id = %q, want 'TF0001'", items[0].SourceID)
	}
}

func TestDiscoverFestivals_SkipsHiddenDirs(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	// .festival hidden dir — should be excluded
	os.MkdirAll(filepath.Join(root, "festivals/active/.festival"), 0755)

	items, err := discoverFestivals(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 festivals (hidden excluded), got %d", len(items))
	}
}

func TestDiscover_FullIntegration(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	// Intent
	writeFile(t, filepath.Join(root, ".campaign/intents/inbox/idea.md"),
		"---\nid: idea-1\ntitle: My Idea\nstatus: inbox\ncreated_at: 2026-01-15\n---\n\n# My Idea")

	// Design
	designDir := filepath.Join(root, "workflow/design/auth-system")
	os.MkdirAll(designDir, 0755)
	writeFile(t, filepath.Join(designDir, "README.md"), "# Auth System\n\nDesign.")

	// Festival
	festDir := filepath.Join(root, "festivals/planning/auth-fest-AF0001")
	os.MkdirAll(festDir, 0755)
	writeFile(t, filepath.Join(festDir, "fest.yaml"),
		"version: \"1.0\"\nmetadata:\n  id: AF0001\n  name: auth-fest\n  festival_type: implementation\n  created_at: 2026-02-01T00:00:00Z\n")

	items, err := Discover(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}

	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	// Verify items are sorted (most recent first)
	for i := 1; i < len(items); i++ {
		prev := items[i-1]
		curr := items[i]
		if prev.SortTimestamp.Before(curr.SortTimestamp) {
			t.Errorf("items not sorted: [%d]=%v before [%d]=%v", i-1, prev.SortTimestamp, i, curr.SortTimestamp)
		}
	}

	// Verify types present
	types := map[WorkflowType]bool{}
	for _, item := range items {
		types[item.WorkflowType] = true
	}
	for _, wt := range []WorkflowType{WorkflowTypeIntent, WorkflowTypeDesign, WorkflowTypeFestival} {
		if !types[wt] {
			t.Errorf("missing workflow type %q in results", wt)
		}
	}
}

func TestDiscover_MissingDirectoriesOK(t *testing.T) {
	root := t.TempDir()
	// Completely empty campaign — no directories exist
	resolver := paths.NewResolver(root, config.DefaultCampaignPaths())

	items, err := Discover(context.Background(), root, resolver)
	if err != nil {
		t.Fatalf("Discover on empty campaign should not error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items for empty campaign, got %d", len(items))
	}
}

func TestFindFestivalPrimaryDoc_Priority(t *testing.T) {
	dir := t.TempDir()

	// No docs
	if got := findFestivalPrimaryDoc(dir); got != "" {
		t.Errorf("expected empty, got %q", got)
	}

	// Only fest.yaml
	writeFile(t, filepath.Join(dir, "fest.yaml"), "version: 1")
	if got := findFestivalPrimaryDoc(dir); filepath.Base(got) != "fest.yaml" {
		t.Errorf("expected fest.yaml, got %q", got)
	}

	// Add FESTIVAL_OVERVIEW.md — should take priority
	writeFile(t, filepath.Join(dir, "FESTIVAL_OVERVIEW.md"), "# Overview")
	if got := findFestivalPrimaryDoc(dir); filepath.Base(got) != "FESTIVAL_OVERVIEW.md" {
		t.Errorf("expected FESTIVAL_OVERVIEW.md, got %q", got)
	}

	// Add FESTIVAL_GOAL.md — should take highest priority
	writeFile(t, filepath.Join(dir, "FESTIVAL_GOAL.md"), "# Goal")
	if got := findFestivalPrimaryDoc(dir); filepath.Base(got) != "FESTIVAL_GOAL.md" {
		t.Errorf("expected FESTIVAL_GOAL.md, got %q", got)
	}
}

func TestDiscoverDesign_WithMetadata(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	designDir := filepath.Join(root, "workflow/design/with-metadata")
	os.MkdirAll(designDir, 0755)
	writeFile(t, filepath.Join(designDir, "README.md"), "# Derived Title\n\nDesc.")
	writeFile(t, filepath.Join(designDir, ".workitem"), `version: 1
kind: workitem
id: design-with-metadata-001
type: design
title: Metadata Title
description: From .workitem.
priority:
  level: high
  reason: launch
execution:
  mode: design
  autonomy: constrained
  risk: medium
`)

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0]
	if it.StableID != "design-with-metadata-001" {
		t.Errorf("StableID = %q", it.StableID)
	}
	if it.Title != "Metadata Title" {
		t.Errorf("Title = %q, want metadata override", it.Title)
	}
	if it.Description != "From .workitem." {
		t.Errorf("Description = %q", it.Description)
	}
	if it.PriorityInfo == nil || it.PriorityInfo.Level != "high" {
		t.Errorf("PriorityInfo = %+v", it.PriorityInfo)
	}
	if it.Execution == nil || it.Execution.Mode != "design" || it.Execution.Autonomy != "constrained" || it.Execution.Risk != "medium" {
		t.Errorf("Execution = %+v", it.Execution)
	}
	if it.RelativePath != "workflow/design/with-metadata" {
		t.Errorf("RelativePath = %q (filesystem placement should win)", it.RelativePath)
	}
}

func TestDiscoverDesign_NoMetadataUnchanged(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	designDir := filepath.Join(root, "workflow/design/legacy")
	os.MkdirAll(designDir, 0755)
	writeFile(t, filepath.Join(designDir, "README.md"), "# Legacy\n\nNo metadata.")

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	it := items[0]
	if it.StableID != "" {
		t.Errorf("StableID should be empty for no-metadata item, got %q", it.StableID)
	}
	if it.Execution != nil || it.PriorityInfo != nil || it.Project != nil || it.WorkflowMeta != nil || it.Lineage != nil {
		t.Errorf("metadata blocks should be nil for no-metadata item, got %+v", it)
	}
	if it.Title != "Legacy" {
		t.Errorf("Title = %q (derived from README heading expected)", it.Title)
	}
}

func TestDiscoverDesign_MalformedMetadataDoesNotCrashDiscovery(t *testing.T) {
	root, resolver := setupTestCampaign(t)
	ctx := context.Background()

	// Item with malformed .workitem (wrong schema version)
	brokenDir := filepath.Join(root, "workflow/design/broken")
	os.MkdirAll(brokenDir, 0755)
	writeFile(t, filepath.Join(brokenDir, "README.md"), "# Broken")
	writeFile(t, filepath.Join(brokenDir, ".workitem"), `version: 2
kind: workitem
id: x
type: design
title: T
`)

	// Sibling item with valid metadata — must still be discovered with metadata applied
	goodDir := filepath.Join(root, "workflow/design/good")
	os.MkdirAll(goodDir, 0755)
	writeFile(t, filepath.Join(goodDir, "README.md"), "# Good")
	writeFile(t, filepath.Join(goodDir, ".workitem"), `version: 1
kind: workitem
id: good-001
type: design
title: Good
`)

	items, err := discoverDesign(ctx, root, resolver)
	if err != nil {
		t.Fatalf("malformed optional metadata must not abort discovery: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items (broken kept with derived fields, good with metadata), got %d", len(items))
	}

	var broken, good *WorkItem
	for i := range items {
		switch filepath.Base(items[i].RelativePath) {
		case "broken":
			broken = &items[i]
		case "good":
			good = &items[i]
		}
	}
	if broken == nil || good == nil {
		t.Fatalf("missing expected items: %+v", items)
	}
	// Broken item: derived fields kept, metadata fields not applied
	if broken.StableID != "" || broken.Execution != nil || broken.PriorityInfo != nil {
		t.Errorf("broken item should have no metadata applied, got %+v", broken)
	}
	if broken.Title != "Broken" {
		t.Errorf("broken item title = %q, want derived heading 'Broken'", broken.Title)
	}
	// Good item: metadata applied normally
	if good.StableID != "good-001" {
		t.Errorf("good item StableID = %q", good.StableID)
	}
}

func TestTimestampDerivation(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-1 * time.Hour)

	t.Run("prefers updated when present", func(t *testing.T) {
		ts := DeriveSortTimestamp(now, earlier)
		if !ts.Equal(now) {
			t.Errorf("expected updated_at, got %v", ts)
		}
	})

	t.Run("falls back to created when updated is zero", func(t *testing.T) {
		ts := DeriveSortTimestamp(time.Time{}, earlier)
		if !ts.Equal(earlier) {
			t.Errorf("expected created_at, got %v", ts)
		}
	})
}
