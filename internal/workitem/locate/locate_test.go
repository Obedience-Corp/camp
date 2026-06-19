package locate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetectFromCwd_ResolvesSymlinkedRoot(t *testing.T) {
	real := t.TempDir()
	wiDir := filepath.Join(real, "workflow", "design", "slug")
	if err := os.MkdirAll(wiDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	link := filepath.Join(t.TempDir(), "campaign-link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	loc, err := DetectFromCwd(link, wiDir)
	if err != nil {
		t.Fatalf("DetectFromCwd with symlinked root: %v", err)
	}
	if loc.Type != "design" || loc.Slug != "slug" {
		t.Fatalf("loc = %+v, want design/slug", loc)
	}
}

func TestDetectFromCwd(t *testing.T) {
	const root = "/campaign"

	tests := []struct {
		name     string
		cwd      string
		wantErr  string
		wantType string
		wantSlug string
		wantSrc  string
		wantPar  string
		wantDun  string
		wantIn   bool
		wantStat string
	}{
		{
			name:     "active workitem root",
			cwd:      "/campaign/workflow/design/myslug",
			wantType: "design",
			wantSlug: "myslug",
			wantSrc:  "/campaign/workflow/design/myslug",
			wantPar:  "/campaign/workflow/design",
			wantDun:  "/campaign/workflow/design/dungeon",
		},
		{
			name:     "active workitem subdir",
			cwd:      "/campaign/workflow/design/myslug/notes",
			wantType: "design",
			wantSlug: "myslug",
			wantSrc:  "/campaign/workflow/design/myslug",
			wantPar:  "/campaign/workflow/design",
			wantDun:  "/campaign/workflow/design/dungeon",
		},
		{
			name:     "active workitem deep subdir",
			cwd:      "/campaign/workflow/explore/topic/a/b/c",
			wantType: "explore",
			wantSlug: "topic",
			wantSrc:  "/campaign/workflow/explore/topic",
			wantPar:  "/campaign/workflow/explore",
			wantDun:  "/campaign/workflow/explore/dungeon",
		},
		{
			name:     "dungeon legacy flat layout",
			cwd:      "/campaign/workflow/design/dungeon/completed/oldslug",
			wantType: "design",
			wantSlug: "oldslug",
			wantSrc:  "/campaign/workflow/design/dungeon/completed/oldslug",
			wantPar:  "/campaign/workflow/design/dungeon/completed",
			wantDun:  "/campaign/workflow/design/dungeon",
			wantIn:   true,
			wantStat: "completed",
		},
		{
			name:     "dungeon dated layout",
			cwd:      "/campaign/workflow/design/dungeon/archived/2026-05-22/oldslug",
			wantType: "design",
			wantSlug: "oldslug",
			wantSrc:  "/campaign/workflow/design/dungeon/archived/2026-05-22/oldslug",
			wantPar:  "/campaign/workflow/design/dungeon/archived/2026-05-22",
			wantDun:  "/campaign/workflow/design/dungeon",
			wantIn:   true,
			wantStat: "archived",
		},
		{
			name:     "dungeon dated subdir",
			cwd:      "/campaign/workflow/design/dungeon/someday/2026-05-22/oldslug/notes",
			wantType: "design",
			wantSlug: "oldslug",
			wantSrc:  "/campaign/workflow/design/dungeon/someday/2026-05-22/oldslug",
			wantPar:  "/campaign/workflow/design/dungeon/someday/2026-05-22",
			wantDun:  "/campaign/workflow/design/dungeon",
			wantIn:   true,
			wantStat: "someday",
		},
		{
			name:    "cwd at campaign root",
			cwd:     "/campaign",
			wantErr: "not inside a workitem",
		},
		{
			name:    "cwd outside campaign root",
			cwd:     "/somewhere/else",
			wantErr: "not under campaign root",
		},
		{
			name:    "cwd outside workflow",
			cwd:     "/campaign/docs/handbook",
			wantErr: "must be under workflow",
		},
		{
			name:    "cwd at workflow root",
			cwd:     "/campaign/workflow",
			wantErr: "not inside a workitem",
		},
		{
			name:    "cwd at workflow type root",
			cwd:     "/campaign/workflow/design",
			wantErr: "not inside a workitem",
		},
		{
			name:    "workflow/dungeon as type",
			cwd:     "/campaign/workflow/dungeon/whatever",
			wantErr: "not a valid workflow type",
		},
		{
			name:    "dungeon root no status",
			cwd:     "/campaign/workflow/design/dungeon",
			wantErr: "without a slug",
		},
		{
			name:    "dungeon status no slug",
			cwd:     "/campaign/workflow/design/dungeon/completed",
			wantErr: "without a slug",
		},
		{
			name:    "dungeon date dir no slug",
			cwd:     "/campaign/workflow/design/dungeon/completed/2026-05-22",
			wantErr: "without a slug",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectFromCwd(root, tc.cwd)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (result=%+v)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Type != tc.wantType {
				t.Errorf("Type=%q want %q", got.Type, tc.wantType)
			}
			if got.Slug != tc.wantSlug {
				t.Errorf("Slug=%q want %q", got.Slug, tc.wantSlug)
			}
			if filepath.ToSlash(got.SourcePath) != tc.wantSrc {
				t.Errorf("SourcePath=%q want %q", got.SourcePath, tc.wantSrc)
			}
			if filepath.ToSlash(got.ParentPath) != tc.wantPar {
				t.Errorf("ParentPath=%q want %q", got.ParentPath, tc.wantPar)
			}
			if filepath.ToSlash(got.DungeonPath) != tc.wantDun {
				t.Errorf("DungeonPath=%q want %q", got.DungeonPath, tc.wantDun)
			}
			if got.InDungeon != tc.wantIn {
				t.Errorf("InDungeon=%v want %v", got.InDungeon, tc.wantIn)
			}
			if got.Status != tc.wantStat {
				t.Errorf("Status=%q want %q", got.Status, tc.wantStat)
			}
		})
	}
}
