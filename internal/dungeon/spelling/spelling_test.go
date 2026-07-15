package spelling

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func TestResolve_BothSpellingsIsAConflict(t *testing.T) {
	parent := t.TempDir()
	mustMkdir(t, filepath.Join(parent, Visible))
	mustMkdir(t, filepath.Join(parent, Hidden))

	_, err := Resolve(context.Background(), parent)
	if err == nil {
		t.Fatal("Resolve() error = nil, want a conflict: resolving either spelling hides the other")
	}
	if !errors.Is(err, camperrors.ErrConflict) {
		t.Errorf("errors.Is(err, ErrConflict) = false, want true (err = %v)", err)
	}

	var conflict *ConflictError
	if !errors.As(err, &conflict) {
		t.Fatalf("errors.As(err, *ConflictError) = false, want true (err = %v)", err)
	}
	if conflict.Parent != parent {
		t.Errorf("Parent = %q, want %q", conflict.Parent, parent)
	}
	if !strings.Contains(err.Error(), MigrateCommand) {
		t.Errorf("error must tell the user how to get unstuck by naming %q, got: %v", MigrateCommand, err)
	}
}

func TestResolve_ConflictErrorIsMatchesByParent(t *testing.T) {
	err := error(NewConflict("/a/b"))
	if !errors.Is(err, &ConflictError{Parent: "/a/b"}) {
		t.Error("conflict should match a ConflictError with the same parent")
	}
	if errors.Is(err, &ConflictError{Parent: "/other"}) {
		t.Error("conflict should not match a ConflictError with a different parent")
	}
	if !errors.Is(err, &ConflictError{}) {
		t.Error("conflict should match a parentless ConflictError probe")
	}
}

func TestResolve(t *testing.T) {
	tests := []struct {
		name       string
		setup      func(t *testing.T, parent string)
		wantName   string
		wantExists bool
	}{
		{
			name:       "neither spelling exists",
			setup:      func(t *testing.T, parent string) {},
			wantName:   Visible,
			wantExists: false,
		},
		{
			name: "only visible exists",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Visible))
			},
			wantName:   Visible,
			wantExists: true,
		},
		{
			name: "only hidden exists",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Hidden))
			},
			wantName:   Hidden,
			wantExists: true,
		},
		{
			name: "non-directory file named dungeon is ignored",
			setup: func(t *testing.T, parent string) {
				if err := os.WriteFile(filepath.Join(parent, Visible), []byte("x"), 0644); err != nil {
					t.Fatalf("writing file: %v", err)
				}
			},
			wantName:   Visible,
			wantExists: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			tt.setup(t, parent)

			got, err := Resolve(context.Background(), parent)
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			if got.Name != tt.wantName {
				t.Errorf("Name = %q, want %q", got.Name, tt.wantName)
			}
			if got.Exists != tt.wantExists {
				t.Errorf("Exists = %v, want %v", got.Exists, tt.wantExists)
			}
			wantPath := filepath.Join(parent, tt.wantName)
			if got.Path != wantPath {
				t.Errorf("Path = %q, want %q", got.Path, wantPath)
			}
		})
	}
}

func TestResolve_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Resolve(ctx, t.TempDir())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

func TestNameForNew_RejectsUnresolvedCampaignSpelling(t *testing.T) {
	for _, campaignName := range []string{"", "Dungeon", "hidden", "true"} {
		t.Run("campaignName="+campaignName, func(t *testing.T) {
			_, err := NameForNew(context.Background(), t.TempDir(), campaignName)
			if err == nil {
				t.Fatalf("NameForNew(%q) error = nil, want an error: the caller must resolve the campaign spelling first", campaignName)
			}
			if !errors.Is(err, camperrors.ErrInvalidInput) {
				t.Errorf("errors.Is(err, ErrInvalidInput) = false, want true (err = %v)", err)
			}
		})
	}
}

func TestNameForNew_ConflictPropagates(t *testing.T) {
	parent := t.TempDir()
	mustMkdir(t, filepath.Join(parent, Visible))
	mustMkdir(t, filepath.Join(parent, Hidden))

	if _, err := NameForNew(context.Background(), parent, Hidden); !errors.Is(err, camperrors.ErrConflict) {
		t.Fatalf("NameForNew() error = %v, want a conflict", err)
	}
}

func TestNameForNew(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T, parent string)
		campaignName string
		want         string
	}{
		{
			name:         "nothing established, campaign is hidden",
			setup:        func(t *testing.T, parent string) {},
			campaignName: Hidden,
			want:         Hidden,
		},
		{
			// The regression the migrate model exists to prevent: a new
			// directory inside a legacy campaign must not adopt the hidden
			// spelling just because dungeon_hidden defaults to true.
			name:         "nothing established, campaign is visible",
			setup:        func(t *testing.T, parent string) {},
			campaignName: Visible,
			want:         Visible,
		},
		{
			name: "visible already exists under parent wins over a hidden campaign",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Visible))
			},
			campaignName: Hidden,
			want:         Visible,
		},
		{
			name: "hidden already exists under parent wins over a visible campaign",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Hidden))
			},
			campaignName: Visible,
			want:         Hidden,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			tt.setup(t, parent)

			got, err := NameForNew(context.Background(), parent, tt.campaignName)
			if err != nil {
				t.Fatalf("NameForNew() error = %v", err)
			}
			if got != tt.want {
				t.Errorf("NameForNew() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRewriteRel(t *testing.T) {
	tests := []struct {
		name       string
		rel        string
		dungeonDir string
		want       string
	}{
		{name: "bare dungeon segment", rel: "dungeon", dungeonDir: Hidden, want: Hidden},
		{name: "nested dungeon segment", rel: "dungeon/done", dungeonDir: Hidden, want: filepath.Join(Hidden, "done")},
		{name: "deeply nested segment", rel: "dungeon/completed/2026-01-01", dungeonDir: Hidden, want: filepath.Join(Hidden, "completed", "2026-01-01")},
		{name: "non-dungeon path untouched", rel: "active", dungeonDir: Hidden, want: "active"},
		{name: "path merely prefixed with dungeon untouched", rel: "dungeonfall", dungeonDir: Hidden, want: "dungeonfall"},
		{name: "visible requested is a no-op", rel: "dungeon/done", dungeonDir: Visible, want: filepath.Join(Visible, "done")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RewriteRel(tt.rel, tt.dungeonDir)
			if got != tt.want {
				t.Errorf("RewriteRel(%q, %q) = %q, want %q", tt.rel, tt.dungeonDir, got, tt.want)
			}
		})
	}
}

func TestIsDungeonName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{name: Visible, want: true},
		{name: Hidden, want: true},
		{name: "active", want: false},
		{name: "", want: false},
	}
	for _, tt := range tests {
		if got := IsDungeonName(tt.name); got != tt.want {
			t.Errorf("IsDungeonName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestNameFor(t *testing.T) {
	if got := NameFor(true); got != Hidden {
		t.Errorf("NameFor(true) = %q, want %q", got, Hidden)
	}
	if got := NameFor(false); got != Visible {
		t.Errorf("NameFor(false) = %q, want %q", got, Visible)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
