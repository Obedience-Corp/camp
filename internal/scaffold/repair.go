package scaffold

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
	"github.com/obediencecorp/camp/internal/config"
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

// RepairPlan holds all proposed changes for a repair operation.
type RepairPlan struct {
	// Changes lists all proposed changes in display order.
	Changes []RepairChange
	// MergedJumps is the computed jumps config after repair (preserving user entries).
	MergedJumps *config.JumpsConfig
}

// HasChanges returns true if the plan contains any additions or modifications.
func (p *RepairPlan) HasChanges() bool {
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

	return plan, nil
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

	// Identify user-defined shortcuts (source != "auto" or source empty on legacy entries).
	for _, key := range sortedKeys(existing.Shortcuts) {
		sc := existing.Shortcuts[key]
		if isUserDefined(sc) {
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
// Legacy entries without a Source field are treated as user-defined (safe default).
func isUserDefined(sc config.ShortcutConfig) bool {
	return sc.Source != config.ShortcutSourceAuto
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
