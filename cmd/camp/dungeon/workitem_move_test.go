package dungeon

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
	hiddenTrue, hiddenFalse := true, false
	item := wkitem.WorkItem{
		RelativePath: "workflow/feature/foo",
		ItemKind:     wkitem.ItemKindDirectory,
	}

	tests := []struct {
		name         string
		hidden       *bool
		rootDungeon  string
		typeDungeon  string
		wantName     string
		wantConflict bool
	}{
		{
			name:        "type-local hidden wins over campaign visible",
			rootDungeon: spelling.Visible,
			typeDungeon: spelling.Hidden,
			wantName:    spelling.Hidden,
		},
		{
			name:        "type-local visible wins over campaign hidden",
			rootDungeon: spelling.Hidden,
			typeDungeon: spelling.Visible,
			wantName:    spelling.Visible,
		},
		{
			name:        "no type dungeon follows hidden campaign",
			rootDungeon: spelling.Hidden,
			wantName:    spelling.Hidden,
		},
		{
			name:        "no type dungeon follows visible campaign",
			rootDungeon: spelling.Visible,
			wantName:    spelling.Visible,
		},
		{
			name:     "empty campaign honors hidden default",
			hidden:   &hiddenTrue,
			wantName: spelling.Hidden,
		},
		{
			name:     "empty campaign honors visible default",
			hidden:   &hiddenFalse,
			wantName: spelling.Visible,
		},
		{
			name:         "both type-local spellings are a conflict",
			typeDungeon:  "both",
			wantConflict: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateGlobalConfig(t, tt.hidden)
			root := t.TempDir()
			typeDir := filepath.Join(root, "workflow", "feature")
			mkdirAll(t, typeDir)

			switch tt.rootDungeon {
			case spelling.Hidden, spelling.Visible:
				mkdirAll(t, filepath.Join(root, tt.rootDungeon))
			}
			switch tt.typeDungeon {
			case spelling.Hidden, spelling.Visible:
				mkdirAll(t, filepath.Join(typeDir, tt.typeDungeon))
			case "both":
				mkdirAll(t, filepath.Join(typeDir, spelling.Hidden))
				mkdirAll(t, filepath.Join(typeDir, spelling.Visible))
			}

			got, err := resolveWorkitemDungeonTarget(context.Background(), root, item)
			if tt.wantConflict {
				if err == nil {
					t.Fatalf("resolveWorkitemDungeonTarget() DungeonPath = %q, want conflict", got.DungeonPath)
				}
				if !camperrors.Is(err, camperrors.ErrConflict) {
					t.Fatalf("resolveWorkitemDungeonTarget() error = %v, want ErrConflict", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveWorkitemDungeonTarget() error = %v", err)
			}
			if got.ItemName != "foo" {
				t.Fatalf("ItemName = %q, want foo", got.ItemName)
			}
			if got.ParentPath != typeDir {
				t.Fatalf("ParentPath = %q, want %q", got.ParentPath, typeDir)
			}
			wantDungeon := filepath.Join(typeDir, tt.wantName)
			if got.DungeonPath != wantDungeon {
				t.Fatalf("DungeonPath = %q, want %q", got.DungeonPath, wantDungeon)
			}
			wantSource := filepath.Join(typeDir, "foo")
			if got.SourcePath != wantSource {
				t.Fatalf("SourcePath = %q, want %q", got.SourcePath, wantSource)
			}
		})
	}
}

func TestResolveWorkitemDungeonTarget_ContextCancelled(t *testing.T) {
	isolateGlobalConfig(t, nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	item := wkitem.WorkItem{
		RelativePath: "workflow/feature/foo",
		ItemKind:     wkitem.ItemKindDirectory,
	}
	_, err := resolveWorkitemDungeonTarget(ctx, t.TempDir(), item)
	if err == nil {
		t.Fatal("expected context cancellation error")
	}
	if !camperrors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
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
		{
			RelativePath: "workflow/feature/.dungeon",
			ItemKind:     wkitem.ItemKindDirectory,
		},
		{
			RelativePath: "workflow/dungeon/foo",
			ItemKind:     wkitem.ItemKindDirectory,
		},
	}

	for _, item := range tests {
		_, err := resolveWorkitemDungeonTarget(context.Background(), "/campaign", item)
		if err == nil {
			t.Fatalf("expected unsupported item error for %+v", item)
		}
		if !strings.Contains(err.Error(), "workflow/<type>/<slug>") {
			t.Fatalf("expected workflow path guidance, got: %s", err)
		}
	}
}

func isolateGlobalConfig(t *testing.T, hidden *bool) {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	if hidden == nil {
		return
	}
	cfgDir := filepath.Join(xdg, "obey", "campaign")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{"dungeon_hidden": %v}`, *hidden)
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mkdirAll(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}
