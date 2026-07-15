package spelling

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	tests := []struct {
		name        string
		setup       func(t *testing.T, parent string)
		wantName    string
		wantExists  bool
		wantWarning bool
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
			name: "both exist prefers visible and warns",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Visible))
				mustMkdir(t, filepath.Join(parent, Hidden))
			},
			wantName:    Visible,
			wantExists:  true,
			wantWarning: true,
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
			if (got.Warning != "") != tt.wantWarning {
				t.Errorf("Warning = %q, wantWarning = %v", got.Warning, tt.wantWarning)
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

func TestNameForNew(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(t *testing.T, parent string)
		hidden bool
		want   string
	}{
		{
			name:   "neither exists, hidden requested",
			setup:  func(t *testing.T, parent string) {},
			hidden: true,
			want:   Hidden,
		},
		{
			name:   "neither exists, visible requested",
			setup:  func(t *testing.T, parent string) {},
			hidden: false,
			want:   Visible,
		},
		{
			name: "visible already exists, hidden requested keeps visible",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Visible))
			},
			hidden: true,
			want:   Visible,
		},
		{
			name: "hidden already exists, visible requested keeps hidden",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Hidden))
			},
			hidden: false,
			want:   Hidden,
		},
		{
			name: "both exist keeps visible regardless of request",
			setup: func(t *testing.T, parent string) {
				mustMkdir(t, filepath.Join(parent, Visible))
				mustMkdir(t, filepath.Join(parent, Hidden))
			},
			hidden: true,
			want:   Visible,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			tt.setup(t, parent)

			got, err := NameForNew(context.Background(), parent, tt.hidden)
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

func TestWarnIfConflicting(t *testing.T) {
	t.Run("no warning is a no-op", func(t *testing.T) {
		var buf bytes.Buffer
		WarnIfConflicting(&buf, Resolved{})
		if buf.Len() != 0 {
			t.Errorf("expected no output, got %q", buf.String())
		}
	})

	t.Run("warning is written", func(t *testing.T) {
		var buf bytes.Buffer
		WarnIfConflicting(&buf, Resolved{Warning: "both exist"})
		if got := buf.String(); got != "warning: both exist\n" {
			t.Errorf("output = %q, want %q", got, "warning: both exist\n")
		}
	})
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
