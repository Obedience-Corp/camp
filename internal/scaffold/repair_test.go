package scaffold

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Obedience-Corp/camp/internal/config"
)

func TestRepairPlan_HasChanges(t *testing.T) {
	tests := []struct {
		name       string
		changes    []RepairChange
		migrations []MigrationAction
		want       bool
	}{
		{
			name: "empty plan has no changes",
			want: false,
		},
		{
			name: "only preserve entries means no changes",
			changes: []RepairChange{
				{Type: RepairPreserve, Key: "x"},
			},
			want: false,
		},
		{
			name: "add entry means has changes",
			changes: []RepairChange{
				{Type: RepairAdd, Key: "new"},
			},
			want: true,
		},
		{
			name: "modify entry means has changes",
			changes: []RepairChange{
				{Type: RepairModify, Key: "mod"},
			},
			want: true,
		},
		{
			name: "mixed preserve and add",
			changes: []RepairChange{
				{Type: RepairPreserve, Key: "x"},
				{Type: RepairAdd, Key: "y"},
			},
			want: true,
		},
		{
			name: "migrations only means has changes",
			migrations: []MigrationAction{
				{Source: "/a/completed", Dest: "/a/dungeon/completed", Items: []string{"item1"}},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := &RepairPlan{Changes: tt.changes, Migrations: tt.migrations}
			if got := plan.HasChanges(); got != tt.want {
				t.Errorf("HasChanges() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsUserDefined(t *testing.T) {
	tests := []struct {
		name   string
		source string
		want   bool
	}{
		{name: "auto source is not user-defined", source: config.ShortcutSourceAuto, want: false},
		{name: "user source is user-defined", source: config.ShortcutSourceUser, want: true},
		{name: "empty source is user-defined (legacy)", source: "", want: true},
		{name: "unknown source is user-defined", source: "custom", want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sc := config.ShortcutConfig{Source: tt.source}
			if got := isUserDefined(sc); got != tt.want {
				t.Errorf("isUserDefined(source=%q) = %v, want %v", tt.source, got, tt.want)
			}
		})
	}
}

func TestSortedKeys(t *testing.T) {
	m := map[string]config.ShortcutConfig{
		"z": {Description: "z"},
		"a": {Description: "a"},
		"m": {Description: "m"},
	}
	got := sortedKeys(m)
	want := []string{"a", "m", "z"}
	if len(got) != len(want) {
		t.Fatalf("len(sortedKeys) = %d, want %d", len(got), len(want))
	}
	for i, k := range got {
		if k != want[i] {
			t.Errorf("sortedKeys[%d] = %q, want %q", i, k, want[i])
		}
	}
}

func TestComputeJumpsChanges_NoExistingConfig(t *testing.T) {
	ctx := context.Background()

	// Create a temp dir with no jumps.yaml
	dir := t.TempDir()
	setupCampaignDir(t, dir)

	plan := &RepairPlan{}
	if err := computeJumpsChanges(ctx, dir, plan); err != nil {
		t.Fatalf("computeJumpsChanges() error: %v", err)
	}

	// All defaults should be added
	defaults := config.DefaultNavigationShortcuts()
	addCount := 0
	for _, c := range plan.Changes {
		if c.Type == RepairAdd && c.Category == "shortcut" {
			addCount++
		}
	}
	if addCount != len(defaults) {
		t.Errorf("expected %d add changes, got %d", len(defaults), addCount)
	}

	if plan.MergedJumps == nil {
		t.Fatal("MergedJumps should not be nil")
	}
}

func TestComputeJumpsChanges_ExistingWithUserShortcuts(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	setupCampaignDir(t, dir)

	// Create a jumps.yaml with a mix of auto and user shortcuts
	existing := &config.JumpsConfig{
		Paths: config.DefaultCampaignPaths(),
		Shortcuts: map[string]config.ShortcutConfig{
			"p": {Path: "projects/", Description: "Projects", Source: config.ShortcutSourceAuto},
			"x": {Path: "custom/", Description: "My custom shortcut", Source: config.ShortcutSourceUser},
			"y": {Path: "another/", Description: "Legacy shortcut"}, // no source = treated as user
		},
	}
	if err := config.SaveJumpsConfig(ctx, dir, existing); err != nil {
		t.Fatalf("SaveJumpsConfig() error: %v", err)
	}

	plan := &RepairPlan{}
	if err := computeJumpsChanges(ctx, dir, plan); err != nil {
		t.Fatalf("computeJumpsChanges() error: %v", err)
	}

	// Check that user shortcuts are preserved
	preserveCount := 0
	addCount := 0
	var preservedKeys []string
	var addedKeys []string
	for _, c := range plan.Changes {
		if c.Category != "shortcut" {
			continue
		}
		switch c.Type {
		case RepairPreserve:
			preserveCount++
			preservedKeys = append(preservedKeys, c.Key)
		case RepairAdd:
			addCount++
			addedKeys = append(addedKeys, c.Key)
		}
	}

	// "x" and "y" should be preserved (user-defined)
	if preserveCount != 2 {
		t.Errorf("expected 2 preserved shortcuts, got %d: %v", preserveCount, preservedKeys)
	}

	// All defaults except "p" should be added (it already exists)
	defaults := config.DefaultNavigationShortcuts()
	expectedAdds := len(defaults) - 1 // minus "p" which exists
	if addCount != expectedAdds {
		t.Errorf("expected %d added shortcuts, got %d: %v", expectedAdds, addCount, addedKeys)
	}

	// Merged config should contain all entries:
	// all 12 defaults + 2 user-defined ("x" and "y") that aren't in defaults = 14
	if plan.MergedJumps == nil {
		t.Fatal("MergedJumps should not be nil")
	}
	totalExpected := len(defaults) + 2
	if len(plan.MergedJumps.Shortcuts) != totalExpected {
		t.Errorf("merged shortcuts count = %d, want %d", len(plan.MergedJumps.Shortcuts), totalExpected)
	}

	// Verify user shortcuts survived in the merge
	if _, ok := plan.MergedJumps.Shortcuts["x"]; !ok {
		t.Error("user shortcut 'x' missing from merged config")
	}
	if _, ok := plan.MergedJumps.Shortcuts["y"]; !ok {
		t.Error("legacy shortcut 'y' missing from merged config")
	}
}

func TestComputeJumpsChanges_AllDefaultsExist(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	setupCampaignDir(t, dir)

	// Save default jumps config (all auto shortcuts present)
	defaults := config.DefaultJumpsConfig()
	if err := config.SaveJumpsConfig(ctx, dir, &defaults); err != nil {
		t.Fatalf("SaveJumpsConfig() error: %v", err)
	}

	plan := &RepairPlan{}
	if err := computeJumpsChanges(ctx, dir, plan); err != nil {
		t.Fatalf("computeJumpsChanges() error: %v", err)
	}

	// No adds should be needed — all defaults already exist
	for _, c := range plan.Changes {
		if c.Type == RepairAdd && c.Category == "shortcut" {
			t.Errorf("unexpected add for shortcut %q — all defaults should already exist", c.Key)
		}
	}
}

func TestComputeMiscFileChanges_MissingFiles(t *testing.T) {
	dir := t.TempDir()
	setupCampaignDir(t, dir)

	plan := &RepairPlan{}
	computeMiscFileChanges(dir, plan)

	// Both .gitignore and CLAUDE.md should be flagged as missing
	found := map[string]bool{}
	for _, c := range plan.Changes {
		if c.Type == RepairAdd {
			found[c.Key] = true
		}
	}

	if !found[".campaign/.gitignore"] {
		t.Error("expected .campaign/.gitignore to be flagged as missing")
	}
	if !found["CLAUDE.md -> AGENTS.md"] {
		t.Error("expected CLAUDE.md symlink to be flagged as missing")
	}
}

func TestComputeMiscFileChanges_FilesExist(t *testing.T) {
	dir := t.TempDir()
	setupCampaignDir(t, dir)

	// Create the files
	gitignorePath := filepath.Join(dir, config.CampaignDir, ".gitignore")
	if err := os.WriteFile(gitignorePath, []byte("state.yaml\n"), 0644); err != nil {
		t.Fatal(err)
	}
	claudePath := filepath.Join(dir, "CLAUDE.md")
	if err := os.Symlink("AGENTS.md", claudePath); err != nil {
		t.Fatal(err)
	}

	plan := &RepairPlan{}
	computeMiscFileChanges(dir, plan)

	for _, c := range plan.Changes {
		if c.Type == RepairAdd {
			t.Errorf("unexpected add change: %s", c.Key)
		}
	}
}

func TestComputeRepairPlan_FullCampaign(t *testing.T) {
	ctx := context.Background()

	dir := t.TempDir()
	// First create a full campaign
	opts := InitOptions{
		Name:   "test-campaign",
		Type:   config.CampaignTypeProduct,
		Repair: false,
	}
	if _, err := Init(ctx, dir, opts); err != nil {
		t.Fatalf("Init() error: %v", err)
	}

	// Now compute repair plan — should have no changes
	repairOpts := InitOptions{
		Name:   "test-campaign",
		Type:   config.CampaignTypeProduct,
		Repair: true,
	}
	plan, err := ComputeRepairPlan(ctx, dir, repairOpts)
	if err != nil {
		t.Fatalf("ComputeRepairPlan() error: %v", err)
	}

	if plan.HasChanges() {
		t.Error("fresh campaign should have no repair changes")
		for _, c := range plan.Changes {
			if c.Type != RepairPreserve {
				t.Logf("  unexpected: %s %s %s", c.Type, c.Category, c.Key)
			}
		}
	}
}

func TestComputeRepairPlan_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ComputeRepairPlan(ctx, t.TempDir(), InitOptions{Repair: true})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestComputeJumpsChanges_OnlyUserEntries(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	setupCampaignDir(t, dir)

	// Config with only user-defined shortcuts (no auto entries at all).
	existing := &config.JumpsConfig{
		Paths: config.DefaultCampaignPaths(),
		Shortcuts: map[string]config.ShortcutConfig{
			"myapp": {Path: "apps/myapp/", Description: "My app", Source: config.ShortcutSourceUser},
		},
	}
	if err := config.SaveJumpsConfig(ctx, dir, existing); err != nil {
		t.Fatal(err)
	}

	plan := &RepairPlan{}
	if err := computeJumpsChanges(ctx, dir, plan); err != nil {
		t.Fatal(err)
	}

	preserveCount := 0
	addCount := 0
	for _, c := range plan.Changes {
		if c.Category != "shortcut" {
			continue
		}
		if c.Type == RepairPreserve {
			preserveCount++
		}
		if c.Type == RepairAdd {
			addCount++
		}
	}

	// "myapp" should be preserved
	if preserveCount != 1 {
		t.Errorf("expected 1 preserved shortcut, got %d", preserveCount)
	}

	// All 12 defaults should be added
	defaults := config.DefaultNavigationShortcuts()
	if addCount != len(defaults) {
		t.Errorf("expected %d added shortcuts, got %d", len(defaults), addCount)
	}

	// "myapp" must survive in merged config
	if _, ok := plan.MergedJumps.Shortcuts["myapp"]; !ok {
		t.Error("user shortcut 'myapp' missing from merged config")
	}
}

func TestComputeRepairPlan_MissingFiles(t *testing.T) {
	ctx := context.Background()

	// Create a campaign, then delete some files to simulate missing items.
	dir := t.TempDir()
	if _, err := Init(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct}); err != nil {
		t.Fatal(err)
	}

	// Remove .gitignore and CLAUDE.md symlink.
	os.Remove(filepath.Join(dir, config.CampaignDir, ".gitignore"))
	os.Remove(filepath.Join(dir, "CLAUDE.md"))

	plan, err := ComputeRepairPlan(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct, Repair: true})
	if err != nil {
		t.Fatal(err)
	}

	if !plan.HasChanges() {
		t.Error("expected changes after removing files")
	}

	found := map[string]bool{}
	for _, c := range plan.Changes {
		if c.Type == RepairAdd {
			found[c.Key] = true
		}
	}

	if !found[".campaign/.gitignore"] {
		t.Error("expected .campaign/.gitignore to be flagged for creation")
	}
	if !found["CLAUDE.md -> AGENTS.md"] {
		t.Error("expected CLAUDE.md symlink to be flagged for creation")
	}
}

func TestRepairInit_RestoresMissingSkillFiles(t *testing.T) {
	ctx := context.Background()

	// Create a campaign, then delete skill files to simulate drift.
	dir := t.TempDir()
	if _, err := Init(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct}); err != nil {
		t.Fatal(err)
	}

	removed := []string{
		filepath.Join(dir, ".campaign", "skills", "camp-navigation", "SKILL.md"),
		filepath.Join(dir, ".campaign", "skills", "references", "camp-command-contracts.md"),
	}
	for _, path := range removed {
		if err := os.Remove(path); err != nil {
			t.Fatalf("os.Remove(%q) error: %v", path, err)
		}
	}

	plan, err := ComputeRepairPlan(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct, Repair: true})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, c := range plan.Changes {
		if c.Type == RepairAdd {
			found[c.Key] = true
		}
	}
	if !found[".campaign/skills/camp-navigation/SKILL.md"] {
		t.Error("expected missing skill file to be flagged for creation")
	}
	if !found[".campaign/skills/references/camp-command-contracts.md"] {
		t.Error("expected missing shared reference file to be flagged for creation")
	}

	if _, err := Init(ctx, dir, InitOptions{
		Name:       "test",
		Type:       config.CampaignTypeProduct,
		Repair:     true,
		RepairPlan: plan,
	}); err != nil {
		t.Fatalf("Init() repair error: %v", err)
	}

	for _, path := range removed {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected repaired file to exist: %s", path)
		}
	}
}

func TestRepairInit_PreservesUserShortcuts(t *testing.T) {
	ctx := context.Background()

	// Create a full campaign.
	dir := t.TempDir()
	if _, err := Init(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct}); err != nil {
		t.Fatal(err)
	}

	// Add a user-defined shortcut to jumps.yaml.
	jumps, err := config.LoadJumpsConfig(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	jumps.Shortcuts["custom"] = config.ShortcutConfig{
		Path:        "my-stuff/",
		Description: "User custom shortcut",
		Source:      config.ShortcutSourceUser,
	}
	if err := config.SaveJumpsConfig(ctx, dir, jumps); err != nil {
		t.Fatal(err)
	}

	// Compute repair plan — should see user shortcut as preserved.
	plan, err := ComputeRepairPlan(ctx, dir, InitOptions{Name: "test", Type: config.CampaignTypeProduct, Repair: true})
	if err != nil {
		t.Fatal(err)
	}

	preserved := false
	for _, c := range plan.Changes {
		if c.Type == RepairPreserve && c.Key == "custom" {
			preserved = true
		}
	}
	if !preserved {
		t.Error("user shortcut 'custom' should be preserved in repair plan")
	}

	// Apply repair with the plan.
	result, err := Init(ctx, dir, InitOptions{
		Name:       "test",
		Type:       config.CampaignTypeProduct,
		Repair:     true,
		RepairPlan: plan,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = result

	// Verify user shortcut survived after repair.
	after, err := config.LoadJumpsConfig(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	if sc, ok := after.Shortcuts["custom"]; !ok {
		t.Error("user shortcut 'custom' was not preserved after repair")
	} else if sc.Source != config.ShortcutSourceUser {
		t.Errorf("user shortcut source = %q, want %q", sc.Source, config.ShortcutSourceUser)
	}
}

func TestComputeMigrationChanges_DetectsMisplacedCompleted(t *testing.T) {
	dir := t.TempDir()

	// Create a workflow dir with both completed/ and dungeon/completed/
	workflowDir := filepath.Join(dir, "workflow", "code_reviews")
	completedDir := filepath.Join(workflowDir, "completed")
	dungeonCompletedDir := filepath.Join(workflowDir, "dungeon", "completed")

	for _, d := range []string{completedDir, dungeonCompletedDir} {
		if err := os.MkdirAll(d, 0755); err != nil {
			t.Fatal(err)
		}
	}

	// Add items to misplaced completed/
	os.WriteFile(filepath.Join(completedDir, "review-1.md"), []byte("review"), 0644)
	os.WriteFile(filepath.Join(completedDir, "review-2.md"), []byte("review"), 0644)
	os.WriteFile(filepath.Join(completedDir, ".gitkeep"), []byte(""), 0644) // should be excluded

	plan := &RepairPlan{}
	computeMigrationChanges(dir, plan)

	if !plan.HasMigrations() {
		t.Fatal("expected migrations to be detected")
	}

	if len(plan.Migrations) != 1 {
		t.Fatalf("expected 1 migration action, got %d", len(plan.Migrations))
	}

	m := plan.Migrations[0]
	if len(m.Items) != 2 {
		t.Errorf("expected 2 items to migrate, got %d: %v", len(m.Items), m.Items)
	}

	// Check migrate changes were added
	migrateCount := 0
	for _, c := range plan.Changes {
		if c.Type == RepairMigrate {
			migrateCount++
		}
	}
	if migrateCount != 2 {
		t.Errorf("expected 2 migrate changes, got %d", migrateCount)
	}
}

func TestComputeMigrationChanges_NoDungeonCompleted(t *testing.T) {
	dir := t.TempDir()

	// completed/ exists but no dungeon/completed/ — no migration
	completedDir := filepath.Join(dir, "workflow", "reviews", "completed")
	if err := os.MkdirAll(completedDir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(completedDir, "item.md"), []byte("x"), 0644)

	plan := &RepairPlan{}
	computeMigrationChanges(dir, plan)

	if plan.HasMigrations() {
		t.Error("should not detect migrations without dungeon/completed")
	}
}

func TestComputeMigrationChanges_PlannedDungeonCompleted(t *testing.T) {
	dir := t.TempDir()

	// completed/ exists with items
	workflowDir := filepath.Join(dir, "workflow", "design")
	completedDir := filepath.Join(workflowDir, "completed")
	if err := os.MkdirAll(completedDir, 0755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(completedDir, "doc.md"), []byte("x"), 0644)

	// dungeon/completed does NOT exist on disk, but IS planned for creation
	dungeonCompletedPath := filepath.Join(workflowDir, "dungeon", "completed")
	relPath, _ := filepath.Rel(dir, dungeonCompletedPath)

	plan := &RepairPlan{
		Changes: []RepairChange{
			{Type: RepairAdd, Category: "directory", Key: relPath},
		},
	}
	computeMigrationChanges(dir, plan)

	if !plan.HasMigrations() {
		t.Error("should detect migration when dungeon/completed is planned for creation")
	}
}

func TestExecuteMigrations_MovesItems(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "completed")
	dst := filepath.Join(dir, "dungeon", "completed")
	os.MkdirAll(src, 0755)
	os.MkdirAll(dst, 0755)

	// Create items in source
	os.WriteFile(filepath.Join(src, "item1.md"), []byte("one"), 0644)
	os.WriteFile(filepath.Join(src, "item2.md"), []byte("two"), 0644)

	migrations := []MigrationAction{
		{Source: src, Dest: dst, Items: []string{"item1.md", "item2.md"}},
	}

	moved, err := ExecuteMigrations(migrations)
	if err != nil {
		t.Fatalf("ExecuteMigrations() error: %v", err)
	}
	if moved != 2 {
		t.Errorf("moved = %d, want 2", moved)
	}

	// Verify items are in destination
	for _, name := range []string{"item1.md", "item2.md"} {
		if _, err := os.Stat(filepath.Join(dst, name)); err != nil {
			t.Errorf("expected %s in destination, got error: %v", name, err)
		}
	}

	// Source directory should be removed (was empty after move)
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Error("expected source directory to be removed after migration")
	}
}

func TestExecuteMigrations_KeepsSourceWithRemainingItems(t *testing.T) {
	dir := t.TempDir()

	src := filepath.Join(dir, "completed")
	dst := filepath.Join(dir, "dungeon", "completed")
	os.MkdirAll(src, 0755)
	os.MkdirAll(dst, 0755)

	// Create items — only one will be migrated
	os.WriteFile(filepath.Join(src, "migrate-me.md"), []byte("yes"), 0644)
	os.WriteFile(filepath.Join(src, "stay-here.md"), []byte("no"), 0644)

	migrations := []MigrationAction{
		{Source: src, Dest: dst, Items: []string{"migrate-me.md"}},
	}

	moved, err := ExecuteMigrations(migrations)
	if err != nil {
		t.Fatalf("ExecuteMigrations() error: %v", err)
	}
	if moved != 1 {
		t.Errorf("moved = %d, want 1", moved)
	}

	// Source should still exist (has remaining items)
	if _, err := os.Stat(src); err != nil {
		t.Error("source directory should still exist when it has remaining items")
	}
}

// setupCampaignDir creates the minimal .campaign directory structure.
func setupCampaignDir(t *testing.T, dir string) {
	t.Helper()
	campaignDir := filepath.Join(dir, config.CampaignDir)
	settingsDir := filepath.Join(campaignDir, config.SettingsDir)
	if err := os.MkdirAll(settingsDir, 0755); err != nil {
		t.Fatal(err)
	}
}
