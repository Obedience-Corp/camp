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

func TestDetectFromCwd_HiddenDungeon(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	itemDir := filepath.Join(root, "workflow", "design", ".dungeon", "completed", "oldslug")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loc, err := DetectFromCwd(root, itemDir)
	if err != nil {
		t.Fatalf("DetectFromCwd() error = %v", err)
	}
	if loc.Type != "design" || loc.Slug != "oldslug" {
		t.Fatalf("loc = %+v, want design/oldslug", loc)
	}
	if !loc.InDungeon {
		t.Error("InDungeon = false, want true")
	}
	if loc.Status != "completed" {
		t.Errorf("Status = %q, want completed", loc.Status)
	}
	wantDungeon := filepath.Join(root, "workflow", "design", ".dungeon")
	if loc.DungeonPath != wantDungeon {
		t.Errorf("DungeonPath = %q, want %q", loc.DungeonPath, wantDungeon)
	}
}

func TestDetectFromCwd_ActiveItemPointsAtEstablishedHiddenDungeon(t *testing.T) {
	root := t.TempDir()
	root, _ = filepath.EvalSymlinks(root)
	if err := os.MkdirAll(filepath.Join(root, "workflow", "design", ".dungeon"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	itemDir := filepath.Join(root, "workflow", "design", "myslug")
	if err := os.MkdirAll(itemDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	loc, err := DetectFromCwd(root, itemDir)
	if err != nil {
		t.Fatalf("DetectFromCwd() error = %v", err)
	}
	wantDungeon := filepath.Join(root, "workflow", "design", ".dungeon")
	if loc.DungeonPath != wantDungeon {
		t.Errorf("DungeonPath = %q, want %q (established hidden spelling)", loc.DungeonPath, wantDungeon)
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
			wantErr: "not under camp root",
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
		{
			name:     "dungeon dated layout 2026-07-24 regression",
			cwd:      "/campaign/workflow/design/dungeon/completed/2026-07-24/oldslug",
			wantType: "design",
			wantSlug: "oldslug",
			wantSrc:  "/campaign/workflow/design/dungeon/completed/2026-07-24/oldslug",
			wantPar:  "/campaign/workflow/design/dungeon/completed/2026-07-24",
			wantDun:  "/campaign/workflow/design/dungeon",
			wantIn:   true,
			wantStat: "completed",
		},
		// No fixture needed: a missing .workitem reads as unstamped.
		{
			name:    "festivals root",
			cwd:     "/campaign/festivals",
			wantErr: "at the festivals root",
		},
		{
			name:    "festivals non-rail stage",
			cwd:     "/campaign/festivals/planning/some-festival",
			wantErr: "not a rail stage",
		},
		{
			name:    "festivals stage without slug",
			cwd:     "/campaign/festivals/ready",
			wantErr: "without a slug",
		},
		{
			name:    "festivals resident without marker",
			cwd:     "/campaign/festivals/ready/unstamped",
			wantErr: "no .workitem marker",
		},
		{
			name:    "festivals dungeon root",
			cwd:     "/campaign/festivals/.dungeon/completed",
			wantErr: "at the festivals dungeon root",
		},
		{
			name:    "festivals dungeon date dir no slug",
			cwd:     "/campaign/festivals/.dungeon/completed/2026-07-24",
			wantErr: "without a slug",
		},
		{
			name:    "festivals dungeon resident without marker",
			cwd:     "/campaign/festivals/.dungeon/completed/2026-07-24/unstamped",
			wantErr: "no .workitem marker",
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

// stampResident writes a valid .workitem marker with the given workflow type.
func stampResident(t *testing.T, root, relDir, typeName string) string {
	t.Helper()
	dir := filepath.Join(root, filepath.FromSlash(relDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("MkdirAll %s: %v", relDir, err)
	}
	marker := "version: v1alpha8\nkind: workitem\nid: " + typeName + "-resident-2026-07-24\ntype: " + typeName + "\ntitle: Resident\n"
	if err := os.WriteFile(filepath.Join(dir, ".workitem"), []byte(marker), 0o644); err != nil {
		t.Fatalf("write .workitem in %s: %v", relDir, err)
	}
	return dir
}

// Rail layouts cannot be resolved from the path alone; Type comes off the
// marker. Every case must report DungeonPath = festivals/.dungeon.
func TestDetectFromCwd_FestivalResident(t *testing.T) {
	tests := []struct {
		name     string
		relDir   string
		cwdRel   string
		typeName string
		wantSlug string
		wantSrc  string
		wantPar  string
		wantIn   bool
		wantStat string
	}{
		{
			name:     "ready stage",
			relDir:   "festivals/ready/my-item",
			typeName: "design",
			wantSlug: "my-item",
			wantSrc:  "festivals/ready/my-item",
			wantPar:  "festivals/ready",
		},
		{
			name:     "active stage",
			relDir:   "festivals/active/my-item",
			typeName: "design",
			wantSlug: "my-item",
			wantSrc:  "festivals/active/my-item",
			wantPar:  "festivals/active",
		},
		{
			name:     "active stage from a subdir",
			relDir:   "festivals/active/my-item",
			cwdRel:   "festivals/active/my-item/notes",
			typeName: "explore",
			wantSlug: "my-item",
			wantSrc:  "festivals/active/my-item",
			wantPar:  "festivals/active",
		},
		{
			name:     "festival-local dungeon dated layout",
			relDir:   "festivals/.dungeon/completed/2026-07-24/my-item",
			typeName: "design",
			wantSlug: "my-item",
			wantSrc:  "festivals/.dungeon/completed/2026-07-24/my-item",
			wantPar:  "festivals/.dungeon/completed/2026-07-24",
			wantIn:   true,
			wantStat: "completed",
		},
		{
			name:     "festival-local dungeon flat layout",
			relDir:   "festivals/.dungeon/archived/my-item",
			typeName: "design",
			wantSlug: "my-item",
			wantSrc:  "festivals/.dungeon/archived/my-item",
			wantPar:  "festivals/.dungeon/archived",
			wantIn:   true,
			wantStat: "archived",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if resolved, err := filepath.EvalSymlinks(root); err == nil {
				root = resolved
			}
			stampResident(t, root, tc.relDir, tc.typeName)

			cwd := filepath.Join(root, filepath.FromSlash(tc.relDir))
			if tc.cwdRel != "" {
				cwd = filepath.Join(root, filepath.FromSlash(tc.cwdRel))
				if err := os.MkdirAll(cwd, 0o755); err != nil {
					t.Fatalf("MkdirAll %s: %v", tc.cwdRel, err)
				}
			}

			got, err := DetectFromCwd(root, cwd)
			if err != nil {
				t.Fatalf("DetectFromCwd(%s): %v", tc.relDir, err)
			}
			if got.Type != tc.typeName {
				t.Errorf("Type=%q want %q (must come from the .workitem marker)", got.Type, tc.typeName)
			}
			if got.Slug != tc.wantSlug {
				t.Errorf("Slug=%q want %q", got.Slug, tc.wantSlug)
			}
			if want := filepath.Join(root, filepath.FromSlash(tc.wantSrc)); got.SourcePath != want {
				t.Errorf("SourcePath=%q want %q", got.SourcePath, want)
			}
			if want := filepath.Join(root, filepath.FromSlash(tc.wantPar)); got.ParentPath != want {
				t.Errorf("ParentPath=%q want %q", got.ParentPath, want)
			}
			if want := filepath.Join(root, "festivals", ".dungeon"); got.DungeonPath != want {
				t.Errorf("DungeonPath=%q want %q", got.DungeonPath, want)
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

// Two residents in one stage folder resolve to different types, which is only
// possible by reading markers.
func TestDetectFromCwd_FestivalResidentTypeIsNotThePath(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	designDir := stampResident(t, root, "festivals/active/a-design", "design")
	exploreDir := stampResident(t, root, "festivals/active/an-explore", "explore")

	design, err := DetectFromCwd(root, designDir)
	if err != nil {
		t.Fatalf("DetectFromCwd(design resident): %v", err)
	}
	explore, err := DetectFromCwd(root, exploreDir)
	if err != nil {
		t.Fatalf("DetectFromCwd(explore resident): %v", err)
	}
	if design.Type != "design" || explore.Type != "explore" {
		t.Fatalf("types = %q/%q, want design/explore", design.Type, explore.Type)
	}
	if design.ParentPath != explore.ParentPath {
		t.Errorf("residents in one stage disagree on ParentPath: %q vs %q", design.ParentPath, explore.ParentPath)
	}
}

// A workflow item's Type comes from its path, so a disagreeing marker is ignored.
func TestDetectFromCwd_WorkflowUnaffectedByMarker(t *testing.T) {
	root := t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	itemDir := stampResident(t, root, "workflow/design/myslug", "explore")

	loc, err := DetectFromCwd(root, itemDir)
	if err != nil {
		t.Fatalf("DetectFromCwd: %v", err)
	}
	if loc.Type != "design" {
		t.Errorf("Type=%q want design (path wins for workflow items, not the marker)", loc.Type)
	}
	if want := filepath.Join(root, "workflow", "design", "dungeon"); loc.DungeonPath != want {
		t.Errorf("DungeonPath=%q want %q", loc.DungeonPath, want)
	}
	if loc.InDungeon {
		t.Error("InDungeon = true, want false")
	}
}
