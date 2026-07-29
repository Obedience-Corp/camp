package quest

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/dungeon/spelling"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// isolateGlobalConfig points the global config at a throwaway XDG dir so the
// dungeon_hidden default is deterministic and the developer's real config is
// never read or written. When hidden is non-nil it writes that value.
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

func TestResolveDungeonName(t *testing.T) {
	hiddenTrue, hiddenFalse := true, false

	tests := []struct {
		name string
		// hidden is the global dungeon_hidden setting; nil leaves it unset
		// (which resolves to true), and only matters for the empty-campaign cases.
		hidden *bool
		// rootDungeon is the spelling created at the campaign root ("", "dungeon",
		// or ".dungeon"); questDungeon is the spelling(s) under .campaign/quests
		// ("", "dungeon", ".dungeon", or "both").
		rootDungeon  string
		questDungeon string
		want         string
		wantErr      bool
	}{
		{name: "both spellings under quests is a conflict", questDungeon: "both", wantErr: true},
		{name: "existing hidden quest dungeon wins", questDungeon: spelling.Hidden, want: spelling.Hidden},
		{name: "existing visible quest dungeon wins (legacy)", questDungeon: spelling.Visible, want: spelling.Visible},
		{name: "campaign hidden spelling propagates to quests", rootDungeon: spelling.Hidden, want: spelling.Hidden},
		{name: "campaign visible spelling propagates to quests", rootDungeon: spelling.Visible, want: spelling.Visible},
		{name: "empty campaign defaults to hidden", hidden: &hiddenTrue, want: spelling.Hidden},
		{name: "empty campaign honors visible default", hidden: &hiddenFalse, want: spelling.Visible},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isolateGlobalConfig(t, tt.hidden)
			root := t.TempDir()
			questsDir := filepath.Join(root, RootDirName)
			mkdirAll(t, questsDir)

			switch tt.rootDungeon {
			case spelling.Hidden, spelling.Visible:
				mkdirAll(t, filepath.Join(root, tt.rootDungeon))
			}
			switch tt.questDungeon {
			case spelling.Hidden, spelling.Visible:
				mkdirAll(t, filepath.Join(questsDir, tt.questDungeon))
			case "both":
				mkdirAll(t, filepath.Join(questsDir, spelling.Hidden))
				mkdirAll(t, filepath.Join(questsDir, spelling.Visible))
			}

			got, err := resolveDungeonName(context.Background(), root)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("resolveDungeonName() = %q, want conflict error", got)
				}
				if !camperrors.Is(err, camperrors.ErrConflict) {
					t.Fatalf("resolveDungeonName() error = %v, want ErrConflict", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveDungeonName() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("resolveDungeonName() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestDungeonDir_UsesResolvedSpelling confirms DungeonDir joins the resolved
// spelling under the quests directory, so an existing hidden dungeon is never
// shadowed by a visible one.
func TestDungeonDir_UsesResolvedSpelling(t *testing.T) {
	isolateGlobalConfig(t, nil)
	root := t.TempDir()
	mkdirAll(t, filepath.Join(root, RootDirName, spelling.Hidden))

	got, err := DungeonDir(context.Background(), root)
	if err != nil {
		t.Fatalf("DungeonDir() error = %v", err)
	}
	want := filepath.Join(root, RootDirName, spelling.Hidden)
	if got != want {
		t.Fatalf("DungeonDir() = %q, want %q", got, want)
	}
}

// TestEnsureQuestDungeon_DoesNotCreateVisibleBesideHidden is the unit-level
// regression for the reported bug: with a migrated hidden quest dungeon present,
// EnsureQuestDungeon must reuse it and never scaffold a visible dungeon/ beside it.
func TestEnsureQuestDungeon_DoesNotCreateVisibleBesideHidden(t *testing.T) {
	isolateGlobalConfig(t, nil)
	ctx := context.Background()
	root := t.TempDir()
	questsDir := filepath.Join(root, RootDirName)
	mkdirAll(t, filepath.Join(questsDir, spelling.Hidden))

	if _, err := EnsureQuestDungeon(ctx, root); err != nil {
		t.Fatalf("EnsureQuestDungeon() error = %v", err)
	}

	if _, err := os.Stat(filepath.Join(questsDir, spelling.Visible)); !os.IsNotExist(err) {
		t.Fatalf("visible dungeon must not be created beside hidden; stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(questsDir, spelling.Hidden, "completed")); err != nil {
		t.Fatalf("hidden dungeon buckets should be ensured: %v", err)
	}
}
