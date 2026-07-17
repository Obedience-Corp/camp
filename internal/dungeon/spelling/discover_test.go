package spelling

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// relPaths turns discovered dungeons into root-relative paths for comparison.
func relPaths(t *testing.T, root string, found []Dungeon) []string {
	t.Helper()
	out := make([]string, 0, len(found))
	for _, d := range found {
		rel, err := filepath.Rel(root, d.Path)
		if err != nil {
			t.Fatalf("rel %s: %v", d.Path, err)
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestDiscover_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Discover(ctx, t.TempDir()); err == nil {
		t.Fatal("Discover() error = nil, want a cancellation error")
	}
}

// TestDiscover_SkipsProjects is the guard that keeps migration off real repos:
// projects/ holds project checkouts, and a source directory named "dungeon"
// inside one (camp itself has internal/dungeon) is a Go package, not a
// campaign dungeon. Sweeping it would corrupt the project.
func TestDiscover_SkipsProjects(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, Visible))
	mustMkdir(t, filepath.Join(root, ProjectsDir, "camp", "internal", "dungeon"))
	mustMkdir(t, filepath.Join(root, ProjectsDir, "camp", "cmd", "camp", "dungeon"))
	mustMkdir(t, filepath.Join(root, ProjectsDir, "fest", "methodology", "festivals", "dungeon"))

	found, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	got := relPaths(t, root, found)
	if !equalStrings(got, []string{Visible}) {
		t.Errorf("Discover() = %v, want only [%s]: nothing under %s/ may be swept", got, Visible, ProjectsDir)
	}
}

// TestDiscover_SkipsNestedRepos covers the same hazard for a repository that
// happens to sit outside projects/.
func TestDiscover_SkipsNestedRepos(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, Visible))
	nested := filepath.Join(root, "vendored", "somerepo")
	mustMkdir(t, filepath.Join(nested, ".git"))
	mustMkdir(t, filepath.Join(nested, "dungeon"))

	found, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if got := relPaths(t, root, found); !equalStrings(got, []string{Visible}) {
		t.Errorf("Discover() = %v, want only [%s]", got, Visible)
	}
}

// TestDiscover_FindsNestedDungeons pins the real-campaign shape that a
// depth-limited or stop-at-first-hit sweep would miss: an archived work item
// inside a dungeon can carry a dungeon of its own.
func TestDiscover_FindsNestedDungeons(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, Visible))
	mustMkdir(t, filepath.Join(root, "workflow", "design", Visible, "completed", "go-token-counter", Visible))
	mustMkdir(t, filepath.Join(root, ".campaign", "intents", Visible))

	found, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	want := []string{
		".campaign/intents/dungeon",
		"dungeon",
		"workflow/design/dungeon",
		"workflow/design/dungeon/completed/go-token-counter/dungeon",
	}
	if got := relPaths(t, root, found); !equalStrings(got, want) {
		t.Errorf("Discover() = %v, want %v", got, want)
	}
}

func TestDiscover_FindsBothSpellings(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, Hidden))
	mustMkdir(t, filepath.Join(root, "workflow", "design", Visible))

	found, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(found) != 2 {
		t.Fatalf("Discover() found %d dungeons, want 2 (%v)", len(found), relPaths(t, root, found))
	}
	if !found[0].Hidden() {
		t.Errorf("%s should report Hidden() = true", found[0].Path)
	}
	if found[1].Hidden() {
		t.Errorf("%s should report Hidden() = false", found[1].Path)
	}
	if found[1].Parent != filepath.Join(root, "workflow", "design") {
		t.Errorf("Parent = %q, want the holding directory", found[1].Parent)
	}
}

// TestDiscover_NonexistentRoot covers camp init, which resolves the campaign's
// spelling before the scaffold step creates the campaign directory.
func TestDiscover_NonexistentRoot(t *testing.T) {
	found, err := Discover(context.Background(), filepath.Join(t.TempDir(), "not-created-yet"))
	if err != nil {
		t.Fatalf("Discover() error = %v, want nil: a directory that does not exist holds no dungeons", err)
	}
	if len(found) != 0 {
		t.Errorf("Discover() = %v, want none", found)
	}
}

func TestCampaignName_NonexistentRootUsesDefault(t *testing.T) {
	got, err := CampaignName(context.Background(), filepath.Join(t.TempDir(), "not-created-yet"), true)
	if err != nil {
		t.Fatalf("CampaignName() error = %v", err)
	}
	if got != Hidden {
		t.Errorf("CampaignName() = %q, want %q", got, Hidden)
	}
}

func TestDiscover_IgnoresFilesNamedDungeon(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, Visible), []byte("x"), 0644); err != nil {
		t.Fatalf("writing file: %v", err)
	}

	found, err := Discover(context.Background(), root)
	if err != nil {
		t.Fatalf("Discover() error = %v", err)
	}
	if len(found) != 0 {
		t.Errorf("Discover() = %v, want none: a file is not a dungeon", relPaths(t, root, found))
	}
}

func TestCampaignName_RootConflictErrors(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, Visible))
	mustMkdir(t, filepath.Join(root, Hidden))

	if _, err := CampaignName(context.Background(), root, true); !errors.Is(err, camperrors.ErrConflict) {
		t.Fatalf("CampaignName() error = %v, want a conflict", err)
	}
}

func TestCampaignName(t *testing.T) {
	tests := []struct {
		name          string
		setup         func(t *testing.T, root string)
		hiddenDefault bool
		want          string
	}{
		{
			name:          "no dungeon anywhere falls back to the hidden default",
			setup:         func(t *testing.T, root string) {},
			hiddenDefault: true,
			want:          Hidden,
		},
		{
			name:          "no dungeon anywhere honours an explicit visible default",
			setup:         func(t *testing.T, root string) {},
			hiddenDefault: false,
			want:          Visible,
		},
		{
			name: "root dungeon is authoritative over the default",
			setup: func(t *testing.T, root string) {
				mustMkdir(t, filepath.Join(root, Visible))
			},
			hiddenDefault: true,
			want:          Visible,
		},
		{
			name: "hidden root dungeon is authoritative",
			setup: func(t *testing.T, root string) {
				mustMkdir(t, filepath.Join(root, Hidden))
			},
			hiddenDefault: false,
			want:          Hidden,
		},
		{
			name: "no root dungeon sweeps and a visible one anywhere means legacy",
			setup: func(t *testing.T, root string) {
				mustMkdir(t, filepath.Join(root, "workflow", "design", Visible))
			},
			hiddenDefault: true,
			want:          Visible,
		},
		{
			name: "no root dungeon and only hidden ones means migrated",
			setup: func(t *testing.T, root string) {
				mustMkdir(t, filepath.Join(root, "workflow", "design", Hidden))
			},
			hiddenDefault: false,
			want:          Hidden,
		},
		{
			name: "a project dungeon never decides the campaign spelling",
			setup: func(t *testing.T, root string) {
				mustMkdir(t, filepath.Join(root, ProjectsDir, "camp", "internal", "dungeon"))
			},
			hiddenDefault: true,
			want:          Hidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			tt.setup(t, root)

			got, err := CampaignName(context.Background(), root, tt.hiddenDefault)
			if err != nil {
				t.Fatalf("CampaignName() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("CampaignName() = %q, want %q", got, tt.want)
			}
		})
	}
}
