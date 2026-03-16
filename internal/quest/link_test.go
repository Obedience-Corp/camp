package quest

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDetectLinkType(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{".campaign/intents/my-intent.yaml", "intent"},
		{".campaign/intents/nested/deep.yaml", "intent"},
		{"workflow/design/spec.md", "design"},
		{"workflow/explore/notes.md", "explore"},
		{"festivals/active/my-fest/", "festival"},
		{"festivals/planning/new-fest/", "festival"},
		{"projects/camp", "project"},
		{"projects/fest", "project"},
		{"docs/readme.md", "document"},
		{"some/random/path.txt", "document"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DetectLinkType(tt.path)
			if got != tt.expected {
				t.Errorf("DetectLinkType(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestValidateLinkPath(t *testing.T) {
	root := t.TempDir()

	// Create a valid target
	targetDir := filepath.Join(root, "projects", "camp")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatal(err)
	}

	t.Run("valid path", func(t *testing.T) {
		if err := ValidateLinkPath(root, "projects/camp"); err != nil {
			t.Errorf("expected nil error, got: %v", err)
		}
	})

	t.Run("missing path", func(t *testing.T) {
		err := ValidateLinkPath(root, "projects/nonexistent")
		if err == nil {
			t.Error("expected error for missing path")
		}
	})

	t.Run("relative traversal rejected", func(t *testing.T) {
		err := ValidateLinkPath(root, "../outside")
		if err == nil {
			t.Error("expected error for path traversal with ..")
		}
	})

	t.Run("absolute path outside campaign rejected", func(t *testing.T) {
		outside := filepath.Join(root, "..", "outside-campaign")
		if err := os.MkdirAll(outside, 0755); err != nil {
			t.Fatal(err)
		}
		// Convert to relative — this produces ../outside-campaign
		rel, _ := filepath.Rel(root, outside)
		err := ValidateLinkPath(root, rel)
		if err == nil {
			t.Error("expected error for path outside campaign root")
		}
	})
}

func TestAddLink(t *testing.T) {
	q := &Quest{
		ID:        "qst_test",
		Name:      "test",
		Status:    StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	link := Link{
		Path:    "projects/camp",
		Type:    "project",
		AddedAt: time.Now().UTC(),
	}

	t.Run("add first link", func(t *testing.T) {
		if err := AddLink(q, link); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(q.Links) != 1 {
			t.Fatalf("expected 1 link, got %d", len(q.Links))
		}
		if q.Links[0].Path != "projects/camp" {
			t.Errorf("expected path 'projects/camp', got %q", q.Links[0].Path)
		}
	})

	t.Run("duplicate path errors", func(t *testing.T) {
		err := AddLink(q, link)
		if err == nil {
			t.Error("expected error for duplicate link")
		}
	})

	t.Run("different path succeeds", func(t *testing.T) {
		other := Link{
			Path:    ".campaign/intents/foo.yaml",
			Type:    "intent",
			AddedAt: time.Now().UTC(),
		}
		if err := AddLink(q, other); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(q.Links) != 2 {
			t.Fatalf("expected 2 links, got %d", len(q.Links))
		}
	})
}

func TestRemoveLink(t *testing.T) {
	q := &Quest{
		ID:        "qst_test",
		Name:      "test",
		Status:    StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Links: []Link{
			{Path: "projects/camp", Type: "project", AddedAt: time.Now()},
			{Path: ".campaign/intents/foo.yaml", Type: "intent", AddedAt: time.Now()},
		},
	}

	t.Run("remove existing link", func(t *testing.T) {
		if err := RemoveLink(q, "projects/camp"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(q.Links) != 1 {
			t.Fatalf("expected 1 link remaining, got %d", len(q.Links))
		}
		if q.Links[0].Path != ".campaign/intents/foo.yaml" {
			t.Errorf("wrong link remaining: %q", q.Links[0].Path)
		}
	})

	t.Run("remove non-existent link errors", func(t *testing.T) {
		err := RemoveLink(q, "nonexistent/path")
		if err == nil {
			t.Error("expected error for non-existent link")
		}
	})
}

func TestLinksSurviveSaveLoad(t *testing.T) {
	dir := t.TempDir()
	questDir := filepath.Join(dir, "test-quest")
	if err := os.MkdirAll(questDir, 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(questDir, "quest.yaml")

	now := time.Now().UTC().Truncate(time.Second)
	q := &Quest{
		ID:        "qst_20260315_abc123",
		Name:      "test-quest",
		Status:    StatusOpen,
		CreatedAt: now,
		UpdatedAt: now,
		Links: []Link{
			{Path: "projects/camp", Type: "project", AddedAt: now},
			{Path: ".campaign/intents/foo.yaml", Type: "intent", AddedAt: now},
		},
	}

	ctx := context.Background()

	if err := Save(ctx, path, q); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load(ctx, path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if len(loaded.Links) != 2 {
		t.Fatalf("expected 2 links after load, got %d", len(loaded.Links))
	}
	if loaded.Links[0].Path != "projects/camp" {
		t.Errorf("link[0].Path = %q, want 'projects/camp'", loaded.Links[0].Path)
	}
	if loaded.Links[0].Type != "project" {
		t.Errorf("link[0].Type = %q, want 'project'", loaded.Links[0].Type)
	}
	if loaded.Links[1].Path != ".campaign/intents/foo.yaml" {
		t.Errorf("link[1].Path = %q, want '.campaign/intents/foo.yaml'", loaded.Links[1].Path)
	}
	if loaded.Links[1].Type != "intent" {
		t.Errorf("link[1].Type = %q, want 'intent'", loaded.Links[1].Type)
	}
}

func TestCloneDeepCopiesLinks(t *testing.T) {
	q := &Quest{
		ID:        "qst_test",
		Name:      "test",
		Status:    StatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Links: []Link{
			{Path: "projects/camp", Type: "project", AddedAt: time.Now()},
		},
	}

	clone := q.Clone()

	// Modify original
	q.Links[0].Path = "modified"

	// Clone should be unaffected
	if clone.Links[0].Path != "projects/camp" {
		t.Errorf("clone was mutated: got %q, want 'projects/camp'", clone.Links[0].Path)
	}
}
