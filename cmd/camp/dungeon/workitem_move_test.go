package dungeon

import (
	"strings"
	"testing"

	wkitem "github.com/Obedience-Corp/camp/internal/workitem"
)

func TestSelectWorkitemDungeonTarget(t *testing.T) {
	items := []wkitem.WorkItem{
		{
			Key:          "feature:workflow/feature/foo",
			StableID:     "feature-foo-fixed",
			RelativePath: "workflow/feature/foo",
			ItemKind:     wkitem.ItemKindDirectory,
		},
		{
			Key:          "bug:workflow/bug/foo",
			StableID:     "bug-foo-fixed",
			RelativePath: "workflow/bug/foo",
			ItemKind:     wkitem.ItemKindDirectory,
		},
	}

	tests := []struct {
		name   string
		target string
		want   string
	}{
		{
			name:   "stable id",
			target: "feature-foo-fixed",
			want:   "workflow/feature/foo",
		},
		{
			name:   "relative path",
			target: "./workflow/bug/foo/",
			want:   "workflow/bug/foo",
		},
		{
			name:   "absolute path",
			target: "/campaign/workflow/feature/foo",
			want:   "workflow/feature/foo",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := selectWorkitemDungeonTarget("/campaign", items, tc.target)
			if err != nil {
				t.Fatalf("selectWorkitemDungeonTarget() error = %v", err)
			}
			if got.RelativePath != tc.want {
				t.Fatalf("RelativePath = %q, want %q", got.RelativePath, tc.want)
			}
		})
	}
}

func TestSelectWorkitemDungeonTarget_AmbiguousSlug(t *testing.T) {
	items := []wkitem.WorkItem{
		{Key: "feature:workflow/feature/foo", RelativePath: "workflow/feature/foo"},
		{Key: "bug:workflow/bug/foo", RelativePath: "workflow/bug/foo"},
	}

	_, err := selectWorkitemDungeonTarget("/campaign", items, "foo")
	if err == nil {
		t.Fatal("expected ambiguity error")
	}
	msg := err.Error()
	for _, want := range []string{
		"ambiguous",
		"workflow/bug/foo",
		"workflow/feature/foo",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("expected error to contain %q, got: %s", want, msg)
		}
	}
}

func TestResolveWorkitemDungeonTarget(t *testing.T) {
	item := wkitem.WorkItem{
		RelativePath: "workflow/feature/foo",
		ItemKind:     wkitem.ItemKindDirectory,
	}

	got, err := resolveWorkitemDungeonTarget("/campaign", item)
	if err != nil {
		t.Fatalf("resolveWorkitemDungeonTarget() error = %v", err)
	}

	if got.ItemName != "foo" {
		t.Fatalf("ItemName = %q, want foo", got.ItemName)
	}
	if got.ParentPath != "/campaign/workflow/feature" {
		t.Fatalf("ParentPath = %q", got.ParentPath)
	}
	if got.DungeonPath != "/campaign/workflow/feature/dungeon" {
		t.Fatalf("DungeonPath = %q", got.DungeonPath)
	}
	if got.SourcePath != "/campaign/workflow/feature/foo" {
		t.Fatalf("SourcePath = %q", got.SourcePath)
	}
}

func TestResolveWorkitemDungeonTarget_RejectsUnsupportedItems(t *testing.T) {
	tests := []wkitem.WorkItem{
		{
			RelativePath: ".campaign/intents/active/foo.md",
			ItemKind:     wkitem.ItemKindFile,
		},
		{
			RelativePath: "festivals/active/demo",
			ItemKind:     wkitem.ItemKindDirectory,
		},
	}

	for _, item := range tests {
		_, err := resolveWorkitemDungeonTarget("/campaign", item)
		if err == nil {
			t.Fatalf("expected unsupported item error for %+v", item)
		}
		if !strings.Contains(err.Error(), "workflow/<type>/<slug>") {
			t.Fatalf("expected workflow path guidance, got: %s", err)
		}
	}
}
