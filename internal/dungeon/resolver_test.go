package dungeon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

func mustEval(t *testing.T, p string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", p, err)
	}
	return resolved
}

func TestResolveContext_NearestDungeon(t *testing.T) {
	campaignRoot := t.TempDir()
	nested := filepath.Join(campaignRoot, "workflow", "design")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatalf("creating nested directory: %v", err)
	}

	rootDungeon := filepath.Join(campaignRoot, "dungeon")
	if err := os.MkdirAll(rootDungeon, 0755); err != nil {
		t.Fatalf("creating root dungeon: %v", err)
	}

	localParent := filepath.Join(campaignRoot, "workflow")
	localDungeon := filepath.Join(localParent, "dungeon")
	if err := os.MkdirAll(localDungeon, 0755); err != nil {
		t.Fatalf("creating local dungeon: %v", err)
	}

	got, err := ResolveContext(context.Background(), campaignRoot, nested)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if got.DungeonPath != mustEval(t, localDungeon) {
		t.Fatalf("DungeonPath = %q, want %q", got.DungeonPath, mustEval(t, localDungeon))
	}
	if got.ParentPath != mustEval(t, localParent) {
		t.Fatalf("ParentPath = %q, want %q", got.ParentPath, mustEval(t, localParent))
	}
}

func TestResolveContext_RootFallback(t *testing.T) {
	campaignRoot := t.TempDir()
	deep := filepath.Join(campaignRoot, "projects", "camp")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatalf("creating deep directory: %v", err)
	}

	rootDungeon := filepath.Join(campaignRoot, "dungeon")
	if err := os.MkdirAll(rootDungeon, 0755); err != nil {
		t.Fatalf("creating root dungeon: %v", err)
	}

	got, err := ResolveContext(context.Background(), campaignRoot, deep)
	if err != nil {
		t.Fatalf("ResolveContext() error = %v", err)
	}

	if got.DungeonPath != mustEval(t, rootDungeon) {
		t.Fatalf("DungeonPath = %q, want %q", got.DungeonPath, mustEval(t, rootDungeon))
	}
	if got.ParentPath != mustEval(t, campaignRoot) {
		t.Fatalf("ParentPath = %q, want %q", got.ParentPath, mustEval(t, campaignRoot))
	}
}

func TestResolveContext_NoDungeonFound(t *testing.T) {
	campaignRoot := t.TempDir()
	inside := filepath.Join(campaignRoot, "workflow", "design")
	if err := os.MkdirAll(inside, 0755); err != nil {
		t.Fatalf("creating inside directory: %v", err)
	}

	_, err := ResolveContext(context.Background(), campaignRoot, inside)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrDungeonContextNotFound) {
		t.Fatalf("error = %v, want ErrDungeonContextNotFound", err)
	}
}

func TestResolveContext_CwdOutsideCampaign(t *testing.T) {
	campaignRoot := t.TempDir()
	outside := t.TempDir()

	_, err := ResolveContext(context.Background(), campaignRoot, outside)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, camperrors.ErrBoundaryViolation) {
		t.Fatalf("error = %v, want boundary violation", err)
	}
}

func TestResolveContext_ContextCancelled(t *testing.T) {
	campaignRoot := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ResolveContext(ctx, campaignRoot, campaignRoot)
	if err == nil {
		t.Fatal("expected cancelled error")
	}
}

func TestResolveContext_TableDrivenLookup(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(t *testing.T, root string) (cwd string, wantContext *Context)
		wantNotFound bool
	}{
		{
			name: "prefers nearest parent dungeon",
			setup: func(t *testing.T, root string) (string, *Context) {
				t.Helper()
				cwd := filepath.Join(root, "a", "b", "c")
				if err := os.MkdirAll(cwd, 0755); err != nil {
					t.Fatalf("mkdir cwd: %v", err)
				}
				nearestParent := filepath.Join(root, "a", "b")
				if err := os.MkdirAll(filepath.Join(nearestParent, "dungeon"), 0755); err != nil {
					t.Fatalf("mkdir nearest dungeon: %v", err)
				}
				if err := os.MkdirAll(filepath.Join(root, "dungeon"), 0755); err != nil {
					t.Fatalf("mkdir root dungeon: %v", err)
				}
				return cwd, &Context{
					DungeonPath: mustEval(t, filepath.Join(nearestParent, "dungeon")),
					ParentPath:  mustEval(t, nearestParent),
				}
			},
		},
		{
			name: "uses root dungeon when no nearer dungeon exists",
			setup: func(t *testing.T, root string) (string, *Context) {
				t.Helper()
				cwd := filepath.Join(root, "x", "y")
				if err := os.MkdirAll(cwd, 0755); err != nil {
					t.Fatalf("mkdir cwd: %v", err)
				}
				rootDungeon := filepath.Join(root, "dungeon")
				if err := os.MkdirAll(rootDungeon, 0755); err != nil {
					t.Fatalf("mkdir root dungeon: %v", err)
				}
				return cwd, &Context{
					DungeonPath: mustEval(t, rootDungeon),
					ParentPath:  mustEval(t, root),
				}
			},
		},
		{
			name: "returns not found when no dungeon in chain",
			setup: func(t *testing.T, root string) (string, *Context) {
				t.Helper()
				cwd := filepath.Join(root, "docs", "api")
				if err := os.MkdirAll(cwd, 0755); err != nil {
					t.Fatalf("mkdir cwd: %v", err)
				}
				return cwd, nil
			},
			wantNotFound: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			cwd, want := tt.setup(t, root)

			got, err := ResolveContext(context.Background(), root, cwd)
			if tt.wantNotFound {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !errors.Is(err, ErrDungeonContextNotFound) {
					t.Fatalf("error = %v, want ErrDungeonContextNotFound", err)
				}
				return
			}

			if err != nil {
				t.Fatalf("ResolveContext() error = %v", err)
			}
			if got.DungeonPath != want.DungeonPath {
				t.Fatalf("DungeonPath = %q, want %q", got.DungeonPath, want.DungeonPath)
			}
			if got.ParentPath != want.ParentPath {
				t.Fatalf("ParentPath = %q, want %q", got.ParentPath, want.ParentPath)
			}
		})
	}
}
