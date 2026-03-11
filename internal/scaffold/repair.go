package scaffold

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
)

// RepairChangeType categorizes a proposed repair change.
type RepairChangeType string

const (
	// RepairAdd indicates a new item will be created.
	RepairAdd RepairChangeType = "add"
	// RepairModify indicates an existing item will be updated.
	RepairModify RepairChangeType = "modify"
	// RepairPreserve indicates a user-defined item will be kept.
	RepairPreserve RepairChangeType = "preserve"
	// RepairMigrate indicates items will be moved to the correct location.
	RepairMigrate RepairChangeType = "migrate"
)

// RepairChange describes a single proposed change during repair.
type RepairChange struct {
	// Type is the kind of change (add, modify, preserve).
	Type RepairChangeType
	// Category groups the change (directory, file, shortcut, config).
	Category string
	// Key is the identifier (path for files/dirs, shortcut name for shortcuts).
	Key string
	// Description explains what will happen.
	Description string
}

// MigrationAction describes items to move from a misplaced directory to its correct location.
type MigrationAction struct {
	// Source is the absolute path to the misplaced directory (e.g., workflow/code_reviews/completed).
	Source string
	// Dest is the absolute path to the correct destination bucket
	// (e.g., workflow/code_reviews/dungeon/completed/YYYY-MM-DD).
	Dest string
	// Items are the names of files/dirs to move.
	Items []string
}

// RepairPlan holds all proposed changes for a repair operation.
type RepairPlan struct {
	// Changes lists all proposed changes in display order.
	Changes []RepairChange
	// MergedJumps is the computed jumps config after repair (preserving user entries).
	MergedJumps *config.JumpsConfig
	// Migrations lists directories whose contents need to be moved.
	Migrations []MigrationAction
}

// HasMigrations returns true if any migration actions are planned.
func (p *RepairPlan) HasMigrations() bool {
	return len(p.Migrations) > 0
}

// HasChanges returns true if the plan contains any additions, modifications, or migrations.
func (p *RepairPlan) HasChanges() bool {
	if p.HasMigrations() {
		return true
	}
	for _, c := range p.Changes {
		if c.Type == RepairAdd || c.Type == RepairModify {
			return true
		}
	}
	return false
}

// ComputeRepairPlan determines what a repair operation would change without applying anything.
func ComputeRepairPlan(ctx context.Context, dir string, opts InitOptions) (*RepairPlan, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	plan := &RepairPlan{}

	// Phase 1: Scaffold dry-run to find missing directories and files.
	if err := computeScaffoldChanges(ctx, absDir, opts, plan); err != nil {
		return nil, err
	}

	// Phase 2: Compute jumps.yaml changes (shortcuts with user-defined preservation).
	if err := computeJumpsChanges(ctx, absDir, plan); err != nil {
		return nil, err
	}

	// Phase 3: Check for missing .gitignore and CLAUDE.md symlink.
	computeMiscFileChanges(absDir, plan)

	// Phase 4: Account for shared standard-dungeon files created outside scaffold FS.
	computeStandardDungeonScaffoldChanges(absDir, plan)

	// Phase 5: Detect misplaced completed/ dirs that should be in dungeon/completed/YYYY-MM-DD/.
	// This runs after scaffold detection so we know which dungeon dirs will be created.
	computeMigrationChanges(absDir, plan)

	return plan, nil
}

func computeStandardDungeonScaffoldChanges(absDir string, plan *RepairPlan) {
	standardDungeonObeys := []string{
		"workflow/code_reviews/dungeon/OBEY.md",
		"workflow/design/dungeon/OBEY.md",
		"workflow/explore/dungeon/OBEY.md",
		"workflow/pipelines/dungeon/OBEY.md",
	}

	seen := make(map[string]bool, len(plan.Changes))
	for _, change := range plan.Changes {
		seen[change.Key] = true
	}

	for _, relPath := range standardDungeonObeys {
		absPath := filepath.Join(absDir, relPath)
		if _, err := os.Stat(absPath); err == nil {
			continue
		}
		if seen[relPath] {
			continue
		}

		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "file",
			Key:         relPath,
			Description: "missing file",
		})
	}
}

// computeScaffoldChanges runs the scaffold in dry mode to identify missing directories and files.
func computeScaffoldChanges(ctx context.Context, absDir string, opts InitOptions, plan *RepairPlan) error {
	name := opts.Name
	if name == "" {
		name = filepath.Base(absDir)
	}

	campaignID := ""
	existingCfg, err := config.LoadCampaignConfig(ctx, absDir)
	if err == nil && existingCfg.ID != "" {
		campaignID = existingCfg.ID
	}

	scaffoldPath := "campaign/scaffold.yaml"
	templateFS, err := fs.Sub(CampaignScaffoldFS, "campaign/templates")
	if err != nil {
		return err
	}

	stats, err := scaffold.ScaffoldFromFS(ctx, CampaignScaffoldFS, scaffoldPath, scaffold.Options{
		TemplatesFS: templateFS,
		Dest:        absDir,
		Vars: map[string]any{
			"campaign_name": name,
			"campaign_id":   campaignID,
			"campaign_type": string(opts.Type),
		},
		Dry:       true,
		Overwrite: false,
	})
	if err != nil {
		return err
	}

	for _, d := range stats.CreatedDirs {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "directory",
			Key:         d,
			Description: "missing directory",
		})
	}

	for _, f := range stats.CreatedFiles {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "file",
			Key:         f,
			Description: "missing file",
		})
	}

	return nil
}

// computeJumpsChanges compares existing jumps.yaml shortcuts with defaults,
// preserving user-defined entries and identifying new auto shortcuts to add.
func computeJumpsChanges(ctx context.Context, absDir string, plan *RepairPlan) error {
	existing, err := config.LoadJumpsConfig(ctx, absDir)
	if err != nil {
		return err
	}

	defaults := config.DefaultJumpsConfig()

	// No existing jumps.yaml — everything from defaults is new.
	if existing == nil {
		plan.MergedJumps = &defaults
		for _, key := range sortedKeys(defaults.Shortcuts) {
			sc := defaults.Shortcuts[key]
			plan.Changes = append(plan.Changes, RepairChange{
				Type:        RepairAdd,
				Category:    "shortcut",
				Key:         key,
				Description: sc.Description,
			})
		}
		return nil
	}

	// Merge: start from existing, add missing auto shortcuts.
	merged := &config.JumpsConfig{
		Paths:     existing.Paths,
		Shortcuts: make(map[string]config.ShortcutConfig),
	}
	merged.ApplyDefaults()

	// Copy all existing shortcuts into merged.
	for k, v := range existing.Shortcuts {
		merged.Shortcuts[k] = v
	}

	// Identify user-defined shortcuts — only show truly user-defined ones.
	// Default shortcuts that already exist and match are silently kept.
	defShortcuts := config.DefaultNavigationShortcuts()
	for _, key := range sortedKeys(existing.Shortcuts) {
		sc := existing.Shortcuts[key]
		if isUserDefined(sc, key, defShortcuts) {
			plan.Changes = append(plan.Changes, RepairChange{
				Type:        RepairPreserve,
				Category:    "shortcut",
				Key:         key,
				Description: sc.Description,
			})
		}
	}

	// Add default shortcuts that don't exist in the current config.
	for _, key := range sortedKeys(defaults.Shortcuts) {
		if _, exists := existing.Shortcuts[key]; !exists {
			merged.Shortcuts[key] = defaults.Shortcuts[key]
			plan.Changes = append(plan.Changes, RepairChange{
				Type:        RepairAdd,
				Category:    "shortcut",
				Key:         key,
				Description: defaults.Shortcuts[key].Description,
			})
		}
	}

	plan.MergedJumps = merged
	return nil
}

// computeMiscFileChanges checks for missing .gitignore and CLAUDE.md symlink.
func computeMiscFileChanges(absDir string, plan *RepairPlan) {
	gitignorePath := filepath.Join(absDir, config.CampaignDir, ".gitignore")
	if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "file",
			Key:         ".campaign/.gitignore",
			Description: "missing gitignore",
		})
	}

	claudePath := filepath.Join(absDir, "CLAUDE.md")
	if _, err := os.Lstat(claudePath); os.IsNotExist(err) {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "file",
			Key:         "CLAUDE.md -> AGENTS.md",
			Description: "missing symlink",
		})
	}
}

// isUserDefined returns true if a shortcut was added by the user (not auto-generated).
// Legacy entries (empty Source) are checked against known defaults before being classified.
func isUserDefined(sc config.ShortcutConfig, key string, defaults map[string]config.ShortcutConfig) bool {
	if sc.Source == config.ShortcutSourceAuto {
		return false
	}
	if sc.Source == config.ShortcutSourceUser {
		return true
	}
	// Legacy (empty Source): check if it matches a known default
	if def, ok := defaults[key]; ok {
		return !shortcutMatchesDefault(sc, def)
	}
	return true // Unknown shortcut with no source → treat as user-defined
}

// shortcutMatchesDefault returns true if a shortcut matches a default by path and concept.
func shortcutMatchesDefault(sc, def config.ShortcutConfig) bool {
	return sc.Path == def.Path && sc.Concept == def.Concept
}

// computeMigrationChanges walks the campaign tree looking for directories
// that have both completed/ at root level and dungeon/completed/ as a subdirectory.
// Items in completed/ should be migrated into dungeon/completed/YYYY-MM-DD/.
// This also considers dungeon dirs that will be created by scaffold repair.
func computeMigrationChanges(absDir string, plan *RepairPlan) {
	// Collect dirs that scaffold will create, so we can account for
	// dungeon/completed dirs that don't exist yet but will after repair.
	plannedDirs := make(map[string]bool)
	for _, c := range plan.Changes {
		if c.Category == "directory" {
			plannedDirs[filepath.Join(absDir, c.Key)] = true
		}
	}

	// Walk the campaign looking for completed/ + dungeon/completed/ pairs.
	_ = filepath.WalkDir(absDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() {
			return nil
		}

		name := d.Name()

		// Skip system directories
		if name == ".git" || name == "node_modules" || name == ".campaign" || name == "dungeon" {
			return filepath.SkipDir
		}

		// Check: does this directory have both completed/ and dungeon/?
		completedPath := filepath.Join(path, "completed")
		dungeonCompletedPath := filepath.Join(path, "dungeon", "completed")

		completedInfo, completedErr := os.Stat(completedPath)
		if completedErr != nil || !completedInfo.IsDir() {
			return nil
		}

		// dungeon/completed must exist on disk OR be planned for creation
		dungeonCompletedExists := false
		if info, err := os.Stat(dungeonCompletedPath); err == nil && info.IsDir() {
			dungeonCompletedExists = true
		}
		if plannedDirs[dungeonCompletedPath] {
			dungeonCompletedExists = true
		}

		if !dungeonCompletedExists {
			return nil
		}

		// List items in completed/ (excluding .gitkeep)
		entries, err := os.ReadDir(completedPath)
		if err != nil {
			return nil
		}

		var items []string
		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}
			items = append(items, entry.Name())
		}

		if len(items) == 0 {
			return nil
		}

		datedDestPath := statuspath.DatedDir(dungeonCompletedPath, time.Now())
		relSource, _ := filepath.Rel(absDir, completedPath)
		relDest, _ := filepath.Rel(absDir, datedDestPath)

		plan.Migrations = append(plan.Migrations, MigrationAction{
			Source: completedPath,
			Dest:   datedDestPath,
			Items:  items,
		})

		for _, item := range items {
			plan.Changes = append(plan.Changes, RepairChange{
				Type:        RepairMigrate,
				Category:    "migration",
				Key:         filepath.Join(relSource, item),
				Description: "→ " + relDest,
			})
		}

		return nil
	})
}

// ExecuteMigrations moves items from misplaced directories to their correct locations.
// Returns the number of items moved and any error.
func ExecuteMigrations(migrations []MigrationAction) (int, error) {
	moved := 0
	for _, m := range migrations {
		// Ensure destination exists
		if err := os.MkdirAll(m.Dest, 0755); err != nil {
			return moved, camperrors.Wrapf(err, "creating %s", m.Dest)
		}

		for _, item := range m.Items {
			src := filepath.Join(m.Source, item)
			dst := filepath.Join(m.Dest, item)

			if err := os.Rename(src, dst); err != nil {
				return moved, camperrors.Wrapf(err, "moving %s to %s", src, dst)
			}
			moved++
		}

		// Remove the now-empty source directory (best effort)
		// Only remove if empty or contains only .gitkeep
		entries, err := os.ReadDir(m.Source)
		if err == nil {
			empty := true
			for _, e := range entries {
				if e.Name() != ".gitkeep" {
					empty = false
					break
				}
			}
			if empty {
				os.RemoveAll(m.Source)
			}
		}
	}
	return moved, nil
}

// sortedKeys returns map keys in sorted order.
func sortedKeys(m map[string]config.ShortcutConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
