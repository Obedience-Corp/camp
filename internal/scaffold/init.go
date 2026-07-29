package scaffold

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	campcontract "github.com/Obedience-Corp/camp/internal/contract"
	dungeonscaffold "github.com/Obedience-Corp/camp/internal/dungeon/scaffold"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/intent"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/version"
	"github.com/Obedience-Corp/obey-shared/contract"
	"github.com/google/uuid"
	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
)

// InitOptions configures the campaign initialization.
type InitOptions struct {
	// Name is the campaign name (defaults to directory name).
	Name string
	// Type is the campaign type.
	Type config.CampaignType
	// Description is a brief description of the campaign.
	Description string
	// Mission is the campaign's mission statement.
	Mission string
	// FromExisting migrates an existing workspace.
	FromExisting bool
	// NoRegister skips adding to global registry.
	NoRegister bool
	// SkipGitInit skips git repository initialization.
	SkipGitInit bool
	// DryRun shows what would be done without creating anything.
	DryRun bool
	// Repair adds missing files to an existing campaign.
	Repair bool
	// SkipSkills disables projecting campaign skills into tool directories.
	SkipSkills bool
	// Org assigns the new campaign to this org on register (created if new).
	// Empty leaves the campaign in the fallback org.
	Org string
	// RepairPlan is the pre-computed repair plan (set by the caller after preview).
	// When set, Init uses the merged jumps config from the plan instead of defaults.
	RepairPlan *RepairPlan
}

// InitResult contains information about what was created.
type InitResult struct {
	// ID is the unique campaign identifier (UUID v4).
	ID string
	// Name is the campaign name.
	Name string
	// CampaignRoot is the path to the campaign root.
	CampaignRoot string
	// DirsCreated lists directories that were created.
	DirsCreated []string
	// FilesCreated lists files that were created.
	FilesCreated []string
	// FilesModified lists existing files that were modified in place.
	FilesModified []string
	// Skipped lists items that were skipped (already exist).
	Skipped []string
	// GitInitialized indicates if a git repository was initialized.
	GitInitialized bool
}

// isInGitRepo checks if the given directory is already inside a git repository.
func isInGitRepo(ctx context.Context, dir string) bool {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--git-dir")
	cmd.Dir = dir
	return cmd.Run() == nil
}

// initGitRepo initializes a new git repository at the given directory.
func initGitRepo(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// Check if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return camperrors.New("git is not installed - use --no-git flag to skip initialization")
	}

	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return camperrors.Wrapf(err, "git init failed (output: %s)", string(output))
	}

	return nil
}

// Init initializes a new campaign at the given directory.
func Init(ctx context.Context, dir string, opts InitOptions) (*InitResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, camperrors.Wrap(err, "failed to resolve path")
	}

	// Check if already inside a campaign
	if _, err := campaign.Detect(ctx, absDir); err == nil {
		if !opts.Repair {
			return nil, camperrors.New(fmt.Sprintf("already inside a campaign at %s\nUse --repair to add missing files", absDir))
		}
		// Repair mode: continue but only create missing files
	}

	// Check if .campaign already exists
	campaignDir := filepath.Join(absDir, config.CampaignDir)
	if _, err := os.Stat(campaignDir); err == nil {
		if !opts.Repair {
			return nil, camperrors.New(fmt.Sprintf("campaign already exists at %s", campaignDir))
		}
		// Repair mode: continue, we'll only create missing files
	}

	// Use directory name as campaign name if not specified
	name := opts.Name
	if name == "" {
		name = filepath.Base(absDir)
	}

	// Default type
	if opts.Type == "" {
		opts.Type = config.CampaignTypeProduct
	}

	// Check if campaign already exists and preserve its ID
	var campaignID string
	existingCfg, err := config.LoadCampaignConfig(ctx, absDir)
	if err == nil && existingCfg.ID != "" {
		// Campaign exists, preserve its ID
		campaignID = existingCfg.ID
	} else {
		// New campaign, generate ID
		campaignID = uuid.New().String()
	}

	result := &InitResult{
		ID:           campaignID,
		Name:         name,
		CampaignRoot: absDir,
	}

	dungeonPlan, err := planDungeonScaffold(ctx, absDir)
	if err != nil {
		return nil, err
	}

	// Scaffold path
	scaffoldPath := "campaign/scaffold.yaml"

	// Run scaffold (handles directories and template files)
	if !opts.DryRun {
		// Use guild-scaffold to create the scaffold structure
		templateFS, err := fs.Sub(CampaignScaffoldFS, "campaign/templates")
		if err != nil {
			return nil, camperrors.Wrap(err, "failed to get template sub-fs")
		}

		stats, scaffoldErr := scaffold.ScaffoldFromFS(ctx, CampaignScaffoldFS, scaffoldPath, scaffold.Options{
			TemplatesFS: templateFS,
			Dest:        absDir,
			Vars: map[string]any{
				"campaign_name":        name,
				"campaign_id":          campaignID,
				"campaign_type":        string(opts.Type),
				"campaign_description": opts.Description,
				"campaign_mission":     opts.Mission,
				"Profile":              version.Profile,
				"now":                  time.Now().UTC().Format(time.RFC3339),
			},
			Dry:       false,
			Overwrite: false,
		})
		if scaffoldErr != nil {
			return nil, camperrors.Wrap(scaffoldErr, "failed to create scaffold")
		}
		if version.Profile == "stable" {
			if err := pruneStableQuestScaffold(absDir, stats); err != nil {
				return nil, err
			}
		}

		// Use scaffold results directly - single source of truth
		for _, dir := range stats.CreatedDirs {
			result.DirsCreated = append(result.DirsCreated, filepath.Join(absDir, dir))
		}
		for _, file := range stats.CreatedFiles {
			result.FilesCreated = append(result.FilesCreated, filepath.Join(absDir, file))
		}
		for _, skipped := range stats.SkippedPaths {
			result.Skipped = append(result.Skipped, filepath.Join(absDir, skipped))
		}

		for i, parent := range dungeonPlan.parents {
			dungeonPath, isNew, err := reconcileDungeonSpelling(result, parent, dungeonPlan.preResolved[i], dungeonPlan.campaignSpelling)
			if err != nil {
				return nil, err
			}

			dungeonResult, err := dungeonscaffold.Init(ctx, dungeonPath, dungeonscaffold.InitOptions{
				Force: isNew,
			})
			if err != nil {
				return nil, camperrors.Wrapf(err, "failed to initialize dungeon scaffold %s", dungeonPath)
			}
			result.DirsCreated = appendUniquePaths(result.DirsCreated, dungeonResult.CreatedDirs...)
			result.FilesCreated = appendUniquePaths(result.FilesCreated, dungeonResult.CreatedFiles...)
			result.Skipped = appendUniquePaths(result.Skipped, dungeonResult.Skipped...)
		}

		// Reconcile the canonical intents dungeon the same way; its own
		// AllStatuses()-driven scaffolding happens later via
		// intent.EnsureDirectories, which must see the correct established
		// spelling by the time it runs.
		if _, _, err := reconcileDungeonSpelling(result, dungeonPlan.intentsParent, dungeonPlan.intentsPre, dungeonPlan.campaignSpelling); err != nil {
			return nil, err
		}

		if version.Profile == "dev" {
			questResult, err := quest.EnsureQuestDungeon(ctx, absDir)
			if err != nil {
				return nil, camperrors.Wrap(err, "failed to initialize quest dungeon")
			}
			result.DirsCreated = appendUniquePaths(result.DirsCreated, questResult.CreatedDirs...)
			result.FilesCreated = appendUniquePaths(result.FilesCreated, questResult.CreatedFiles...)
			result.Skipped = appendUniquePaths(result.Skipped, questResult.Skipped...)
		}
	}

	// Create campaign.yaml (metadata and concepts - paths/shortcuts go in jumps.yaml)
	description := opts.Description
	if description == "" {
		description = fmt.Sprintf("Campaign: %s", name)
	}

	cfg := &config.CampaignConfig{
		ID:          campaignID,
		Name:        name,
		Type:        opts.Type,
		CreatedAt:   time.Now(),
		Description: description,
		Mission:     opts.Mission,
		ConceptList: config.DefaultConcepts(),
		Intents:     config.IntentsConfig{Tags: config.DefaultIntentTags()},
		Workflows:   config.DefaultWorkflowsConfig(),
	}

	// In repair mode, preserve existing config values and merge concepts
	if opts.Repair && existingCfg != nil {
		cfg.CreatedAt = existingCfg.CreatedAt
		cfg.Description = existingCfg.Description
		cfg.Projects = existingCfg.Projects
		cfg.Hooks = existingCfg.Hooks
		// Preserve existing intent tags; otherwise the defaults seeded above
		// fill in the block for campaigns predating it.
		if len(existingCfg.Intents.Tags) > 0 {
			cfg.Intents = existingCfg.Intents
		}
		// Preserve existing mission unless a new one was provided
		if opts.Mission != "" {
			cfg.Mission = opts.Mission
		} else {
			cfg.Mission = existingCfg.Mission
		}
		// Use merged concepts from repair plan (updates stale paths, adds missing defaults)
		if opts.RepairPlan != nil && len(opts.RepairPlan.MergedConcepts) > 0 {
			cfg.ConceptList = opts.RepairPlan.MergedConcepts
		} else if len(existingCfg.ConceptList) > 0 {
			cfg.ConceptList = existingCfg.ConceptList
		}
		// Use merged workflows from repair plan (backfills defaults, preserves user mappings).
		if opts.RepairPlan != nil && opts.RepairPlan.MergedWorkflows != nil {
			cfg.Workflows = *opts.RepairPlan.MergedWorkflows
		} else {
			cfg.Workflows = existingCfg.Workflows
		}
	}

	if !opts.DryRun {
		if err := config.SaveCampaignConfig(ctx, absDir, cfg); err != nil {
			return nil, camperrors.Wrap(err, "failed to create campaign config")
		}
	}
	result.FilesCreated = append(result.FilesCreated, config.CampaignConfigPath(absDir))

	// Repair: rewrite a default quest still stamped with the sentinel date.
	if opts.Repair && !opts.DryRun && opts.RepairPlan != nil {
		if err := applyQuestDateBackfill(ctx, opts.RepairPlan.QuestDateBackfill); err != nil {
			return nil, err
		}
	}

	// Create jumps.yaml (paths and shortcuts).
	// In repair mode with a pre-computed plan, use the merged config that preserves user entries.
	jumps := config.DefaultJumpsConfig()
	if opts.Repair && opts.RepairPlan != nil && opts.RepairPlan.MergedJumps != nil {
		jumps = *opts.RepairPlan.MergedJumps
	}
	if !opts.DryRun {
		if err := config.SaveJumpsConfig(ctx, absDir, &jumps); err != nil {
			return nil, camperrors.Wrap(err, "failed to create jumps config")
		}

		// Reuse the intent service setup path during init/repair so canonical
		// intent migration stays centralized in one helper.
		intentSvc := intent.NewIntentService(absDir, filepath.Join(absDir, jumps.Paths.Intents))
		if err := intentSvc.EnsureDirectories(ctx); err != nil {
			return nil, camperrors.Wrap(err, "failed to initialize intent directories")
		}
		if err := intentSvc.CleanupLegacyIntentScaffold(); err != nil {
			return nil, camperrors.Wrap(err, "failed to remove legacy intent scaffold")
		}
	}
	result.FilesCreated = append(result.FilesCreated, config.JumpsConfigPath(absDir))

	// Write camp's entries to the campaign contract (.campaign/watchers.yaml).
	// This declares camp's state files and directories so the daemon knows what
	// to watch. The WriteEntries protocol is merge-safe: if fest already wrote
	// entries (fest init ran before camp init), camp's entries are added alongside
	// them without clobbering fest's entries.
	if !opts.DryRun {
		contractPath := contract.ContractPath(absDir)
		if err := contract.WriteEntries(contractPath, contract.OwnerCamp, campcontract.CampEntries()); err != nil {
			// Log but don't fail -- contract writing is not critical to camp init.
			// The contract can be regenerated by running camp init --repair.
			fmt.Fprintf(os.Stderr, "Warning: could not write contract entries: %v\n", err)
		} else {
			result.FilesCreated = append(result.FilesCreated, contractPath)
		}
	}

	// Create .gitignore to exclude machine-specific files
	gitignorePath := filepath.Join(campaignDir, ".gitignore")
	if !opts.DryRun {
		if _, err := os.Stat(gitignorePath); os.IsNotExist(err) {
			gitignoreContent := `# Machine-specific state (navigation history, timestamps)
state.yaml

# Generated cache (navigation index, rebuilt automatically)
cache/

# Local campaign event ledger (append-only runtime history)
events/

# Tool-managed state (workitem priorities, regenerated automatically)
settings/workitems.json

# Per-machine current-workitem selection (do not share across machines)
workitems/current.yaml
`
			if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
				return nil, camperrors.Wrap(err, "failed to create .gitignore")
			}
			result.FilesCreated = append(result.FilesCreated, ".campaign/.gitignore")
		} else if opts.Repair {
			for _, entry := range campaignGitignoreRequiredRules {
				if err := appendGitignoreEntryIfMissing(absDir, entry); err != nil {
					return nil, camperrors.Wrapf(err, "failed to append %s to .gitignore", entry)
				}
			}
		}
	}

	if !opts.DryRun {
		worktreesPath := jumps.Paths.Worktrees
		if worktreesPath == "" {
			worktreesPath = config.DefaultCampaignPaths().Worktrees
		}
		created, modified, giErr := ensureRootGitignoreWorktrees(absDir, worktreesPath)
		if giErr != nil {
			return nil, giErr
		}
		gitignoreAbs := filepath.Join(absDir, ".gitignore")
		if created {
			result.FilesCreated = append(result.FilesCreated, gitignoreAbs)
		} else if modified {
			result.FilesModified = append(result.FilesModified, gitignoreAbs)
		}
	}

	// Create CLAUDE.md symlink to AGENTS.md (AGENTS.md is the source of truth)
	claudePath := filepath.Join(absDir, "CLAUDE.md")

	if !opts.DryRun {
		// Create symlink to AGENTS.md (relative path)
		if _, err := os.Lstat(claudePath); os.IsNotExist(err) {
			if err := os.Symlink("AGENTS.md", claudePath); err != nil {
				// Symlinks may fail on some systems, just note it
				result.Skipped = append(result.Skipped, claudePath+" (symlink failed)")
			} else {
				result.FilesCreated = append(result.FilesCreated, claudePath+" -> AGENTS.md")
			}
		}
	}

	// Festival Methodology setup is intentionally not done here. Scaffolding
	// owns the campaign's own directories and files; running the fest CLI is a
	// command-level concern, handled once by each command that initializes a
	// campaign (see initcmd.InitializeFestivals).

	// Initialize git repository if not already in one and not skipped
	if !opts.SkipGitInit && !opts.DryRun {
		if !isInGitRepo(ctx, absDir) {
			if err := initGitRepo(ctx, absDir); err != nil {
				return nil, err
			}
			result.GitInitialized = true
		}
	}

	// Register in global registry
	if !opts.NoRegister && !opts.DryRun {
		if err := config.UpdateRegistry(ctx, func(reg *config.Registry) error {
			return reg.RegisterWithOrg(campaignID, name, absDir, opts.Type, opts.Org)
		}); err != nil {
			return nil, camperrors.Wrap(err, "failed to register campaign")
		}
	}

	return result, nil
}

func pruneStableQuestScaffold(absDir string, stats *scaffold.ScaffoldStats) error {
	if stats == nil {
		return nil
	}
	questRootCreated := containsRelPath(stats.CreatedDirs, quest.RootDirName)
	if questRootCreated {
		if err := os.RemoveAll(filepath.Join(absDir, quest.RootDirName)); err != nil {
			return camperrors.Wrap(err, "remove stable quest scaffold")
		}
	} else {
		for _, rel := range stats.CreatedFiles {
			if isQuestScaffoldRelPath(rel) {
				_ = os.Remove(filepath.Join(absDir, filepath.FromSlash(filepath.ToSlash(rel))))
			}
		}
		dirs := append([]string(nil), stats.CreatedDirs...)
		sort.Slice(dirs, func(i, j int) bool {
			return strings.Count(filepath.ToSlash(dirs[i]), "/") > strings.Count(filepath.ToSlash(dirs[j]), "/")
		})
		for _, rel := range dirs {
			if isQuestScaffoldRelPath(rel) {
				_ = os.Remove(filepath.Join(absDir, filepath.FromSlash(filepath.ToSlash(rel))))
			}
		}
	}

	stats.CreatedDirs = filterOutQuestScaffoldPaths(stats.CreatedDirs)
	stats.CreatedFiles = filterOutQuestScaffoldPaths(stats.CreatedFiles)
	stats.SkippedPaths = filterOutQuestScaffoldPaths(stats.SkippedPaths)
	return nil
}

func filterOutQuestScaffoldPaths(paths []string) []string {
	if len(paths) == 0 {
		return paths
	}
	filtered := paths[:0]
	for _, path := range paths {
		if !isQuestScaffoldRelPath(path) {
			filtered = append(filtered, path)
		}
	}
	return filtered
}

func containsRelPath(paths []string, want string) bool {
	want = filepath.ToSlash(strings.TrimSpace(want))
	for _, path := range paths {
		if filepath.ToSlash(strings.TrimSpace(path)) == want {
			return true
		}
	}
	return false
}

func isQuestScaffoldRelPath(path string) bool {
	normalized := filepath.ToSlash(strings.TrimSpace(path))
	root := filepath.ToSlash(quest.RootDirName)
	return normalized == root || strings.HasPrefix(normalized, root+"/")
}

func appendUniquePaths(existing []string, paths ...string) []string {
	seen := make(map[string]struct{}, len(existing))
	for _, path := range existing {
		seen[path] = struct{}{}
	}
	for _, path := range paths {
		if _, ok := seen[path]; ok {
			continue
		}
		existing = append(existing, path)
		seen[path] = struct{}{}
	}
	return existing
}

// Validate checks if the given options are valid.
func (o *InitOptions) Validate() error {
	if o.Type != "" && !o.Type.Valid() {
		return camperrors.New(fmt.Sprintf("invalid campaign type: %s", o.Type))
	}
	return nil
}

