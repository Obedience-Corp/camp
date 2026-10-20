//go:build container_fs

// These tests initialize, repair, and move quests in real campaigns on disk, so
// they build only under the container_fs tag and run in the integration
// harness's pooled container, never on the host.

package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Obedience-Corp/camp/internal/quest"
)

func newDefaultQuestCamp(t *testing.T, name string) string {
	t.Helper()
	tmpDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", tmpDir)
	dir := filepath.Join(tmpDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Init(t.Context(), dir, InitOptions{Name: name, NoRegister: true, SkipGitInit: true}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	return dir
}

func repairDefaultQuestCamp(t *testing.T, dir string) (*RepairPlan, *InitResult) {
	t.Helper()
	opts := InitOptions{Name: filepath.Base(dir), Repair: true, NoRegister: true, SkipGitInit: true}
	plan, err := ComputeRepairPlan(t.Context(), dir, opts)
	if err != nil {
		t.Fatalf("ComputeRepairPlan() error = %v", err)
	}
	opts.RepairPlan = plan
	result, err := Init(t.Context(), dir, opts)
	if err != nil {
		t.Fatalf("Init() with repair error = %v", err)
	}
	return plan, result
}

func defaultQuests(t *testing.T, dir string) []*quest.Quest {
	t.Helper()
	all, err := quest.List(t.Context(), dir, true)
	if err != nil {
		t.Fatalf("quest.List() error = %v", err)
	}
	var defaults []*quest.Quest
	for _, q := range all {
		if q.IsDefault() {
			defaults = append(defaults, q)
		}
	}
	return defaults
}

func questScaffoldPlanFiles(plan *RepairPlan) []string {
	var files []string
	for _, c := range plan.Changes {
		if c.Type == RepairAdd && c.Category == "file" && strings.HasPrefix(c.Key, quest.RootDirName+"/") {
			files = append(files, c.Key)
		}
	}
	slices.Sort(files)
	return files
}

func questScaffoldCreatedFiles(t *testing.T, dir string, result *InitResult) []string {
	t.Helper()
	var files []string
	for _, abs := range result.FilesCreated {
		rel, err := filepath.Rel(dir, abs)
		if err != nil {
			t.Fatalf("filepath.Rel(%q, %q) error = %v", dir, abs, err)
		}
		rel = filepath.ToSlash(rel)
		if strings.HasPrefix(rel, quest.RootDirName+"/") {
			files = append(files, rel)
		}
	}
	slices.Sort(files)
	return files
}

func assertSingleDefaultQuest(t *testing.T, dir string, wantStatus quest.Status, wantPath string) {
	t.Helper()
	defaults := defaultQuests(t, dir)
	if len(defaults) != 1 {
		paths := make([]string, 0, len(defaults))
		for _, q := range defaults {
			paths = append(paths, q.Path)
		}
		t.Fatalf("found %d %s quests, want 1: %v", len(defaults), quest.DefaultQuestID, paths)
	}
	if defaults[0].Status != wantStatus || defaults[0].Path != wantPath {
		t.Fatalf("default quest = %s at %s, want %s at %s", defaults[0].Status, defaults[0].Path, wantStatus, wantPath)
	}
	resolved, err := quest.Resolve(t.Context(), dir, quest.DefaultQuestID)
	if err != nil {
		t.Fatalf("quest.Resolve(%s) error = %v", quest.DefaultQuestID, err)
	}
	if resolved.Path != wantPath {
		t.Fatalf("quest.Resolve(%s) = %s, want %s", quest.DefaultQuestID, resolved.Path, wantPath)
	}
}

func assertRepairKeepsMovedDefaultQuest(t *testing.T, name string, target quest.Status, move func(context.Context, *quest.Service) error) {
	t.Helper()
	dir := newDefaultQuestCamp(t, name)
	if err := move(t.Context(), quest.NewService(dir)); err != nil {
		t.Fatalf("move default quest to %s: %v", target, err)
	}
	statusDir, err := quest.DungeonStatusDir(t.Context(), dir, target)
	if err != nil {
		t.Fatalf("quest.DungeonStatusDir() error = %v", err)
	}
	movedPath := quest.QuestPathForDir(filepath.Join(statusDir, quest.DefaultDirName))

	plan, result := repairDefaultQuestCamp(t, dir)

	defaultRoot := filepath.Join(quest.RootDirName, quest.DefaultDirName)
	for _, c := range plan.Changes {
		if strings.HasPrefix(c.Key, filepath.ToSlash(defaultRoot)) {
			t.Errorf("repair plan offers %s %s (%s)", c.Type, c.Key, c.Description)
		}
	}
	for _, created := range append(append([]string(nil), result.DirsCreated...), result.FilesCreated...) {
		if strings.HasPrefix(created, filepath.Join(dir, defaultRoot)) {
			t.Errorf("repair created %s", created)
		}
	}
	if _, err := os.Stat(quest.DefaultQuestPath(dir)); !os.IsNotExist(err) {
		t.Fatalf("repair recreated %s, stat err = %v", quest.DefaultQuestPath(dir), err)
	}
	assertSingleDefaultQuest(t, dir, target, movedPath)
}

func TestDefaultQuestScaffold_FreshInitCreatesDefault(t *testing.T) {
	dir := newDefaultQuestCamp(t, "fresh-default-quest")
	assertSingleDefaultQuest(t, dir, quest.StatusOpen, quest.DefaultQuestPath(dir))
}

func TestDefaultQuestScaffold_RepairAfterCompleteKeepsDungeonDefault(t *testing.T) {
	assertRepairKeepsMovedDefaultQuest(t, "repair-completed-default", quest.StatusCompleted,
		func(ctx context.Context, svc *quest.Service) error {
			_, err := svc.Complete(ctx, quest.DefaultQuestName)
			return err
		})
}

func TestDefaultQuestScaffold_RepairAfterArchiveKeepsDungeonDefault(t *testing.T) {
	assertRepairKeepsMovedDefaultQuest(t, "repair-archived-default", quest.StatusArchived,
		func(ctx context.Context, svc *quest.Service) error {
			_, err := svc.Archive(ctx, quest.DefaultQuestName)
			return err
		})
}

func TestInit_RepairRestoresQuestScaffold(t *testing.T) {
	campaignDir := newDefaultQuestCamp(t, "repair-quest")

	removed := []string{
		quest.DefaultQuestPath(campaignDir),
		// Hidden-default campaign: the quest dungeon uses ".dungeon" like the rest.
		filepath.Join(campaignDir, quest.RootDirName, ".dungeon", "OBEY.md"),
	}
	for _, path := range removed {
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove(%q) error = %v", path, err)
		}
	}

	plan, _ := repairDefaultQuestCamp(t, campaignDir)

	wantKey := filepath.ToSlash(filepath.Join(quest.RootDirName, quest.DefaultDirName, quest.FileName))
	if !slices.Contains(questScaffoldPlanFiles(plan), wantKey) {
		t.Errorf("repair plan does not offer the deleted default quest %s", wantKey)
	}
	for _, path := range removed {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected repaired quest scaffold path to exist: %s", path)
		}
	}
	assertSingleDefaultQuest(t, campaignDir, quest.StatusOpen, quest.DefaultQuestPath(campaignDir))

	// Verify .active file is NOT recreated on repair
	activePath := filepath.Join(campaignDir, quest.RootDirName, ".active")
	if _, err := os.Stat(activePath); !os.IsNotExist(err) {
		t.Errorf(".active file should not be created on repair: %s", activePath)
	}
}

func TestDefaultQuestScaffold_RepairPlanMatchesInit(t *testing.T) {
	cases := []struct {
		name        string
		prepare     func(context.Context, string) error
		wantDefault bool
	}{
		{
			name: "completed",
			prepare: func(ctx context.Context, dir string) error {
				_, err := quest.NewService(dir).Complete(ctx, quest.DefaultQuestName)
				return err
			},
		},
		{
			name: "archived",
			prepare: func(ctx context.Context, dir string) error {
				_, err := quest.NewService(dir).Archive(ctx, quest.DefaultQuestName)
				return err
			},
		},
		{
			name: "deleted",
			prepare: func(_ context.Context, dir string) error {
				return os.RemoveAll(filepath.Dir(quest.DefaultQuestPath(dir)))
			},
			wantDefault: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := newDefaultQuestCamp(t, "plan-matches-"+tc.name)
			if err := tc.prepare(t.Context(), dir); err != nil {
				t.Fatalf("prepare: %v", err)
			}
			if err := os.Remove(filepath.Join(dir, quest.RootDirName, ".dungeon", "OBEY.md")); err != nil {
				t.Fatal(err)
			}

			plan, result := repairDefaultQuestCamp(t, dir)

			planned := questScaffoldPlanFiles(plan)
			created := questScaffoldCreatedFiles(t, dir, result)
			if !slices.Equal(planned, created) {
				t.Fatalf("repair plan files %v, Init created %v", planned, created)
			}
			wantKey := filepath.ToSlash(filepath.Join(quest.RootDirName, quest.DefaultDirName, quest.FileName))
			if got := slices.Contains(planned, wantKey); got != tc.wantDefault {
				t.Fatalf("plan offers %s = %t, want %t", wantKey, got, tc.wantDefault)
			}
			if got := len(defaultQuests(t, dir)); got != 1 {
				t.Fatalf("found %d %s quests after repair, want 1", got, quest.DefaultQuestID)
			}
		})
	}
}
