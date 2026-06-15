package scaffold

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/config"
	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	"github.com/Obedience-Corp/camp/internal/dungeon/statuspath"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/statusmove"
	"github.com/Obedience-Corp/camp/internal/version"
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
	// MergedConcepts is the computed concept list after repair (updating stale default paths).
	MergedConcepts []config.ConceptEntry
	// Migrations lists directories whose contents need to be moved.
	Migrations []MigrationAction
	// IntentMigrations lists legacy intent-root moves handled internally by Init.
	IntentMigrations []MigrationAction
}

// HasMigrations returns true if any migration actions are planned.
func (p *RepairPlan) HasMigrations() bool {
	return len(p.Migrations) > 0 || len(p.IntentMigrations) > 0
}

// HasChanges returns true if the plan contains any additions, modifications, or migrations.
func (p *RepairPlan) HasChanges() bool {
	if p.HasMigrations() {
		return true
	}
	for _, c := range p.Changes {
		if c.Type == RepairAdd || c.Type == RepairModify || c.Type == RepairMigrate {
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

	// Phase 2: Compute concept changes (update stale default paths, add missing defaults).
	if err := computeConceptChanges(ctx, absDir, plan); err != nil {
		return nil, err
	}

	// Phase 3: Compute jumps.yaml changes (shortcuts with user-defined preservation).
	if err := computeJumpsChanges(ctx, absDir, plan); err != nil {
		return nil, err
	}

	// Phase 4: Check for missing .gitignore and CLAUDE.md symlink.
	computeMiscFileChanges(absDir, plan)

	// Phase 5: Account for shared standard-dungeon files created outside scaffold FS.
	computeStandardDungeonScaffoldChanges(absDir, plan)

	// Phase 6: Account for imperative quest scaffold files created outside scaffold FS.
	if version.Profile == "dev" {
		computeQuestScaffoldChanges(absDir, plan)
	}

	// Phase 7: Detect misplaced completed/ dirs that should be in dungeon/completed/YYYY-MM-DD/.
	// This runs after scaffold detection so we know which dungeon dirs will be created.
	computeMigrationChanges(absDir, plan)

	// Phase 8: Detect legacy workflow/intents state or scaffold residue that
	// repair will normalize into the canonical .campaign/intents root during Init.
	if err := computeIntentMigrationChanges(absDir, plan); err != nil {
		return nil, err
	}

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

func computeQuestScaffoldChanges(absDir string, plan *RepairPlan) {
	// The quests directory and default/quest.yaml are handled by the scaffold
	// template system (they live under campaign/templates/.campaign/quests/).
	// Only the dungeon subdirectories and their files are created imperatively
	// via dungeonscaffold.Init(), so we derive the expected paths from the
	// same StandardStatuses slice that dungeonscaffold.Init() uses.
	dungeonBase := filepath.Join(quest.RootDirName, "dungeon")
	requiredDirs := []string{filepath.ToSlash(dungeonBase)}
	for _, status := range dungeonscaffold.StandardStatuses {
		requiredDirs = append(requiredDirs, filepath.ToSlash(filepath.Join(dungeonBase, status)))
	}
	requiredFiles := []string{filepath.ToSlash(filepath.Join(dungeonBase, "OBEY.md"))}
	for _, status := range dungeonscaffold.StandardStatuses {
		requiredFiles = append(requiredFiles, filepath.ToSlash(filepath.Join(dungeonBase, status, ".gitkeep")))
	}

	seen := make(map[string]bool, len(plan.Changes))
	for _, change := range plan.Changes {
		seen[change.Key] = true
	}

	for _, relPath := range requiredDirs {
		absPath := filepath.Join(absDir, filepath.FromSlash(relPath))
		if info, err := os.Stat(absPath); err == nil && info.IsDir() {
			continue
		}
		if seen[relPath] {
			continue
		}
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "directory",
			Key:         relPath,
			Description: "missing quest directory",
		})
	}

	for _, relPath := range requiredFiles {
		absPath := filepath.Join(absDir, filepath.FromSlash(relPath))
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
			Description: "missing quest scaffold file",
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
			"Profile":       version.Profile,
		},
		Dry:       true,
		Overwrite: false,
	})
	if err != nil {
		return err
	}
	if version.Profile == "stable" {
		stats.CreatedDirs = filterOutQuestScaffoldPaths(stats.CreatedDirs)
		stats.CreatedFiles = filterOutQuestScaffoldPaths(stats.CreatedFiles)
		stats.SkippedPaths = filterOutQuestScaffoldPaths(stats.SkippedPaths)
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

const conceptWorkflowParent = "workflow"

// computeConceptChanges migrates existing campaign concepts toward the nested
// shape: flat workflow-family concepts move under a single workflow parent,
// missing default top-level concepts and workflow children are added, and
// custom/unknown concepts are preserved. It is idempotent.
func computeConceptChanges(ctx context.Context, absDir string, plan *RepairPlan) error {
	existing, err := config.LoadCampaignConfig(ctx, absDir)
	if err != nil || existing == nil {
		return nil // No campaign.yaml yet; init will create it fresh.
	}

	if len(existing.ConceptList) == 0 {
		return nil // No concepts; init will use defaults.
	}

	before := existing.ConceptList
	hadParent := conceptIndexByName(before, conceptWorkflowParent) != -1

	merged := migrateConceptsToNested(before)
	if !hadParent {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairAdd,
			Category:    "concept",
			Key:         conceptWorkflowParent,
			Description: "Workflows",
		})
	} else if !conceptListsEqual(before, merged) {
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairModify,
			Category:    "concept",
			Key:         conceptWorkflowParent,
			Description: "nest workflow collections under the workflow parent",
		})
	}

	merged = ensureDefaultConcepts(merged, &plan.Changes)

	plan.MergedConcepts = merged
	return nil
}

// workflowFamilyNames returns the concept names that are workflow collections,
// derived from the default workflow parent's children (plus the festival alias)
// so there is no second hardcoded list.
func workflowFamilyNames() map[string]bool {
	names := map[string]bool{"festival": true}
	for _, c := range config.DefaultConcepts() {
		if strings.EqualFold(c.Name, conceptWorkflowParent) {
			for _, ch := range c.Children {
				names[strings.ToLower(ch.Name)] = true
			}
		}
	}
	return names
}

func isWorkflowFamily(c config.ConceptEntry, family map[string]bool) bool {
	return family[strings.ToLower(c.Name)] || strings.HasPrefix(c.Path, "workflow/")
}

func conceptIndexByName(concepts []config.ConceptEntry, name string) int {
	for i := range concepts {
		if strings.EqualFold(concepts[i].Name, name) {
			return i
		}
	}
	return -1
}

// migrateConceptsToNested moves flat workflow-family concepts under a single
// workflow parent, drops the default worktrees entry, and is idempotent.
func migrateConceptsToNested(concepts []config.ConceptEntry) []config.ConceptEntry {
	family := workflowFamilyNames()

	var toNest []config.ConceptEntry
	hasParent := false
	for _, c := range concepts {
		if strings.EqualFold(c.Name, conceptWorkflowParent) {
			hasParent = true
			continue
		}
		if isWorkflowFamily(c, family) {
			toNest = append(toNest, c)
		}
	}
	if len(toNest) == 0 && hasParent {
		return concepts
	}

	nest := func(parent config.ConceptEntry) config.ConceptEntry {
		seen := make(map[string]bool)
		for _, ch := range parent.Children {
			seen[strings.ToLower(ch.Name)] = true
		}
		for _, c := range toNest {
			if seen[strings.ToLower(c.Name)] {
				continue
			}
			parent.Children = append(parent.Children, c)
			seen[strings.ToLower(c.Name)] = true
		}
		return parent
	}

	var result []config.ConceptEntry
	parentEmitted := false
	for _, c := range concepts {
		switch {
		case strings.EqualFold(c.Name, conceptWorkflowParent):
			result = append(result, nest(c))
			parentEmitted = true
		case strings.EqualFold(c.Name, "worktrees") && c.Path == "projects/worktrees/":
			// Drop the default worktrees entry (a projects detail, not a picker concept).
		case isWorkflowFamily(c, family):
			// Moved under the workflow parent.
		default:
			result = append(result, c)
		}
	}
	if !parentEmitted {
		result = append(result, nest(config.ConceptEntry{Name: conceptWorkflowParent, Path: "workflow/", Description: "Workflows"}))
	}
	return result
}

// ensureDefaultConcepts adds any missing default top-level concept and any
// missing default workflow child, preserving existing and custom entries.
func ensureDefaultConcepts(concepts []config.ConceptEntry, changes *[]RepairChange) []config.ConceptEntry {
	for _, def := range config.DefaultConcepts() {
		idx := conceptIndexByName(concepts, def.Name)
		if idx == -1 {
			concepts = append(concepts, def)
			*changes = append(*changes, RepairChange{Type: RepairAdd, Category: "concept", Key: def.Name, Description: def.Description})
			continue
		}
		if !strings.EqualFold(def.Name, conceptWorkflowParent) {
			continue
		}
		parent := concepts[idx]
		seen := make(map[string]bool)
		for _, ch := range parent.Children {
			seen[strings.ToLower(ch.Name)] = true
		}
		for _, defChild := range def.Children {
			if seen[strings.ToLower(defChild.Name)] {
				continue
			}
			parent.Children = append(parent.Children, defChild)
			seen[strings.ToLower(defChild.Name)] = true
			*changes = append(*changes, RepairChange{Type: RepairAdd, Category: "concept", Key: "workflow/" + defChild.Name, Description: defChild.Description})
		}
		concepts[idx] = parent
	}
	return concepts
}

func conceptListsEqual(a, b []config.ConceptEntry) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Name != b[i].Name || a[i].Path != b[i].Path || len(a[i].Children) != len(b[i].Children) {
			return false
		}
	}
	return true
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
	} else if err == nil {
		raw, readErr := os.ReadFile(gitignorePath)
		if readErr == nil && !gitignoreHasRule(string(raw), "workitems/current.yaml") {
			plan.Changes = append(plan.Changes, RepairChange{
				Type:        RepairModify,
				Category:    "file",
				Key:         ".campaign/.gitignore",
				Description: "missing workitems/current.yaml entry",
			})
		}
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

// appendGitignoreEntryIfMissing appends a single line to the campaign
// .gitignore when the entry is not already present. Presence is detected
// using gitignore-line semantics (non-comment, trimmed exact match) so a
// commented-out or substring-match line does not fool the check. The
// operation is append-only and never rewrites the file wholesale.
func appendGitignoreEntryIfMissing(absDir, entry string) error {
	gitignorePath := filepath.Join(absDir, config.CampaignDir, ".gitignore")
	raw, err := os.ReadFile(gitignorePath)
	if err != nil {
		return err
	}
	if gitignoreHasRule(string(raw), entry) {
		return nil
	}
	suffix := "\n"
	if len(raw) > 0 && raw[len(raw)-1] != '\n' {
		suffix = "\n\n"
	}
	addition := suffix + "# Per-machine current-workitem selection (do not share across machines)\n" + entry + "\n"
	// TODO(seq06-lock): concurrent repair runs can still race this read-modify-write append.
	return fsutil.WriteFileAtomically(gitignorePath, append(raw, []byte(addition)...), 0o644)
}

// gitignoreHasRule reports whether content contains an active gitignore
// rule equal to entry. Lines are trimmed of whitespace; comment lines
// (starting with `#`) and blank lines are ignored. Substring matches
// like `not-<entry>` or commented-out `# <entry>` do NOT count as
// present because git would still track the file.
func gitignoreHasRule(content, entry string) bool {
	target := strings.TrimSpace(entry)
	if target == "" {
		return false
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if trimmed == target {
			return true
		}
	}
	return false
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

// knownWorkflowRoots are the campaign workflow directories that may contain
// completed/ items eligible for migration into dungeon/completed/YYYY-MM-DD/.
// Only these roots are scanned, preventing accidental migration of items
// inside projects/* submodules or other unrelated directories.
var knownWorkflowRoots = []string{
	"workflow/code_reviews",
	"workflow/design",
	"workflow/explore",
	"workflow/pipelines",
}

// computeMigrationChanges checks known workflow roots for directories
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

	for _, root := range knownWorkflowRoots {
		rootPath := filepath.Join(absDir, root)
		completedPath := filepath.Join(rootPath, "completed")
		dungeonCompletedPath := filepath.Join(rootPath, "dungeon", "completed")

		completedInfo, completedErr := os.Stat(completedPath)
		if completedErr != nil || !completedInfo.IsDir() {
			continue
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
			continue
		}

		// List items in completed/ (excluding .gitkeep)
		entries, err := os.ReadDir(completedPath)
		if err != nil {
			continue
		}

		var items []string
		for _, entry := range entries {
			if entry.Name() == ".gitkeep" {
				continue
			}
			items = append(items, entry.Name())
		}

		if len(items) == 0 {
			continue
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
	}
}

func computeIntentMigrationChanges(absDir string, plan *RepairPlan) error {
	intentSvc := intent.NewIntentService(absDir, filepath.Join(absDir, config.CampaignDir, "intents"))
	moves, err := intentSvc.PlanLegacyIntentRootMigration()
	if err != nil {
		return err
	}

	for _, move := range moves {
		relSource, err := filepath.Rel(absDir, move.Source)
		if err != nil {
			return camperrors.Wrapf(err, "computing relative path for %s", move.Source)
		}
		relDest, err := filepath.Rel(absDir, move.Dest)
		if err != nil {
			return camperrors.Wrapf(err, "computing relative path for %s", move.Dest)
		}

		plan.IntentMigrations = append(plan.IntentMigrations, MigrationAction{
			Source: filepath.Dir(move.Source),
			Dest:   filepath.Dir(move.Dest),
			Items:  []string{filepath.Base(move.Source)},
		})
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairMigrate,
			Category:    "intent_migration",
			Key:         filepath.ToSlash(relSource),
			Description: "→ " + filepath.ToSlash(relDest),
		})
	}

	cleanupPaths, err := intentSvc.PlanLegacyIntentRootCleanup()
	if err != nil {
		return err
	}
	for _, cleanupPath := range cleanupPaths {
		relPath, err := filepath.Rel(absDir, cleanupPath)
		if err != nil {
			return camperrors.Wrapf(err, "computing relative path for %s", cleanupPath)
		}
		plan.Changes = append(plan.Changes, RepairChange{
			Type:        RepairModify,
			Category:    "intent_cleanup",
			Key:         filepath.ToSlash(relPath),
			Description: "remove legacy workflow/intents scaffold after canonical normalization",
		})
	}

	return nil
}

type migrationMover func(src, dst string) error

// ExecuteMigrations moves items from misplaced directories to their correct locations.
// It validates every source and destination before moving anything, and returns
// the number of items moved plus any error.
func ExecuteMigrations(migrations []MigrationAction) (int, error) {
	return executeMigrations(migrations, executeMigrationMove)
}

func executeMigrations(migrations []MigrationAction, moveItem migrationMover) (int, error) {
	if err := validateMigrationPlan(migrations); err != nil {
		return 0, err
	}

	moved := 0
	total := countMigrationPlanItems(migrations)
	for _, m := range migrations {
		// Ensure destination exists
		if err := os.MkdirAll(m.Dest, 0755); err != nil {
			remaining := total - moved
			return moved, camperrors.Wrapf(err,
				"creating destination directory %s (%d item(s) moved; %d item(s) remaining)",
				m.Dest, moved, remaining)
		}

		for _, item := range m.Items {
			src := filepath.Join(m.Source, item)
			dst := filepath.Join(m.Dest, item)

			if err := moveItem(src, dst); err != nil {
				remaining := total - moved
				return moved, camperrors.Wrapf(err,
					"moving %s to %s (%d item(s) moved; %d item(s) remaining)",
					src, dst, moved, remaining)
			}
			moved++
		}

		removeEmptySourceDir(m.Source)
	}
	return moved, nil
}

// validateMigrationPlan checks every (src, dst) pair before any file is moved.
func validateMigrationPlan(migrations []MigrationAction) error {
	var errs []string
	for _, m := range migrations {
		for _, item := range m.Items {
			src := filepath.Join(m.Source, item)
			dst := filepath.Join(m.Dest, item)

			if _, err := os.Stat(src); err != nil {
				if os.IsNotExist(err) {
					errs = append(errs, "source not found: "+src)
				} else {
					errs = append(errs, fmt.Sprintf("cannot stat source %s: %v", src, err))
				}
			}

			if existing, exists, err := statuspath.ExistingItemPath(m.Dest, item); err != nil {
				errs = append(errs, fmt.Sprintf("cannot check destination %s: %v", dst, err))
			} else if exists {
				errs = append(errs, "destination already exists: "+existing)
			}
		}
	}
	if len(errs) > 0 {
		return camperrors.New("migration pre-validation failed:\n  " + strings.Join(errs, "\n  "))
	}
	return nil
}

func executeMigrationMove(src, dst string) error {
	if _, err := statusmove.Move(context.Background(), src, dst, statusmove.MoveOptions{}); err != nil {
		if errors.Is(err, statusmove.ErrAlreadyExists) {
			return camperrors.New("destination already exists: " + dst)
		}
		return err
	}
	return nil
}

func countMigrationPlanItems(migrations []MigrationAction) int {
	total := 0
	for _, m := range migrations {
		total += len(m.Items)
	}
	return total
}

// removeEmptySourceDir removes a source directory only if it is empty or
// contains only .gitkeep. Cleanup is best-effort after successful moves.
func removeEmptySourceDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != ".gitkeep" {
			return
		}
	}
	if err := os.RemoveAll(dir); err != nil {
		fmt.Fprintf(os.Stderr, "camp repair warning: failed to remove legacy dir %s: %v\n", dir, err)
	}
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
