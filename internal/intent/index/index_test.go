package index

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/obediencecorp/camp/internal/intent"
)

func setupTestIntents(t *testing.T) string {
	t.Helper()

	// Create temp directory structure
	tmpDir := t.TempDir()

	// Create status directories
	for _, status := range []string{"inbox", "active", "ready"} {
		if err := os.MkdirAll(filepath.Join(tmpDir, status), 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Create test intents
	intents := map[string]string{
		"inbox/20260129-auth-feature.md": `---
id: 20260129-auth-feature
title: Authentication Feature
status: inbox
created_at: 2026-01-29
type: feature
tags:
  - auth
  - security
---

# Authentication Feature

Implement #login and #auth functionality.
`,
		"inbox/20260128-login-bug.md": `---
id: 20260128-login-bug
title: Login Bug Fix
status: inbox
created_at: 2026-01-28
type: bug
tags:
  - auth
  - bug
---

# Login Bug Fix

Fix the #login issue with session handling.
`,
		"active/20260127-navigation.md": `---
id: 20260127-navigation
title: Navigation System
status: active
created_at: 2026-01-27
type: feature
tags:
  - navigation
  - ui
---

# Navigation System

Improve the #navigation and routing.
`,
		"ready/20260126-unrelated.md": `---
id: 20260126-unrelated
title: Unrelated Feature
status: ready
created_at: 2026-01-26
type: feature
tags:
  - other
---

# Unrelated Feature

Something completely different about #database.
`,
	}

	for path, content := range intents {
		fullPath := filepath.Join(tmpDir, path)
		if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	return tmpDir
}

func TestIndex_Build(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	err := idx.Build(context.Background(), nil)
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Should index 4 intents from inbox, active, ready
	if idx.Size() != 4 {
		t.Errorf("Size() = %d, want 4", idx.Size())
	}

	// BuildTime should be set
	if idx.BuildTime().IsZero() {
		t.Error("BuildTime() should not be zero")
	}
}

func TestIndex_FindByTag(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Find by "auth" tag - should match 2 intents
	authIDs := idx.FindByTag("auth")
	if len(authIDs) != 2 {
		t.Errorf("FindByTag('auth') returned %d results, want 2", len(authIDs))
	}

	// Find by "navigation" tag - should match 1 intent
	navIDs := idx.FindByTag("navigation")
	if len(navIDs) != 1 {
		t.Errorf("FindByTag('navigation') returned %d results, want 1", len(navIDs))
	}

	// Find by non-existent tag
	noneIDs := idx.FindByTag("nonexistent")
	if len(noneIDs) != 0 {
		t.Errorf("FindByTag('nonexistent') returned %d results, want 0", len(noneIDs))
	}
}

func TestIndex_FindByHashtag(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Find by "login" hashtag - should match 2 intents
	loginIDs := idx.FindByHashtag("login")
	if len(loginIDs) != 2 {
		t.Errorf("FindByHashtag('login') returned %d results, want 2", len(loginIDs))
	}

	// Find by "auth" hashtag - should match 1 intent
	authIDs := idx.FindByHashtag("auth")
	if len(authIDs) != 1 {
		t.Errorf("FindByHashtag('auth') returned %d results, want 1", len(authIDs))
	}

	// Find by "navigation" hashtag - should match 1 intent
	navIDs := idx.FindByHashtag("navigation")
	if len(navIDs) != 1 {
		t.Errorf("FindByHashtag('navigation') returned %d results, want 1", len(navIDs))
	}
}

func TestIndex_FindSimilar(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	// Find intents similar to auth-feature
	// Use low threshold (0.01) since test content is small
	similar := idx.FindSimilar("20260129-auth-feature", 0.01)

	// Should find at least some similar intents
	if len(similar) == 0 {
		t.Error("FindSimilar should return at least one result")
	}

	// The login-bug and auth-feature share common terms
	found := false
	for _, s := range similar {
		if s.ID == "20260128-login-bug" {
			found = true
			break
		}
	}
	if !found {
		t.Error("FindSimilar should find login-bug as similar to auth-feature")
	}
}

func TestIndex_GetAllTags(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	tags := idx.GetAllTags()

	// Should have: auth, security, bug, navigation, ui, other
	if len(tags) < 5 {
		t.Errorf("GetAllTags() returned %d tags, want at least 5", len(tags))
	}

	// Tags should be sorted
	for i := 1; i < len(tags); i++ {
		if tags[i-1] > tags[i] {
			t.Error("GetAllTags() should return sorted tags")
			break
		}
	}
}

func TestIndex_GetAllHashtags(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	hashtags := idx.GetAllHashtags()

	// Should have: login, auth, navigation, database
	if len(hashtags) < 3 {
		t.Errorf("GetAllHashtags() returned %d hashtags, want at least 3", len(hashtags))
	}
}

func TestIndex_TagCounts(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	counts := idx.TagCounts()

	// "auth" tag should appear in 2 intents
	if counts["auth"] != 2 {
		t.Errorf("TagCounts()['auth'] = %d, want 2", counts["auth"])
	}
}

func TestIndex_BuildWithSpecificStatuses(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	// Only index inbox
	err := idx.Build(context.Background(), []intent.Status{intent.StatusInbox})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}

	// Should only index 2 intents from inbox
	if idx.Size() != 2 {
		t.Errorf("Size() = %d, want 2 (inbox only)", idx.Size())
	}
}

func TestIndex_GetIndexedIntent(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)
	if err := idx.Build(context.Background(), nil); err != nil {
		t.Fatal(err)
	}

	indexed := idx.GetIndexedIntent("20260129-auth-feature")
	if indexed == nil {
		t.Fatal("GetIndexedIntent() returned nil")
	}

	if indexed.Title != "Authentication Feature" {
		t.Errorf("Title = %q, want %q", indexed.Title, "Authentication Feature")
	}

	if len(indexed.Tags) != 2 {
		t.Errorf("Tags length = %d, want 2", len(indexed.Tags))
	}

	if len(indexed.Hashtags) < 2 {
		t.Errorf("Hashtags length = %d, want at least 2", len(indexed.Hashtags))
	}
}

func TestIndex_ContextCancellation(t *testing.T) {
	tmpDir := setupTestIntents(t)

	idx := NewIndex(tmpDir)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := idx.Build(ctx, nil)
	if err != context.Canceled {
		t.Errorf("Build() error = %v, want context.Canceled", err)
	}
}
