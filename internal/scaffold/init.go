package scaffold

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	campcontract "github.com/Obedience-Corp/camp/internal/contract"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
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
		return fmt.Errorf("git is not installed - use --no-git flag to skip initialization")
	}

	cmd := exec.CommandContext(ctx, "git", "init")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("git init failed: %w (output: %s)", err, string(output))
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
			return nil, fmt.Errorf("already inside a campaign at %s\nUse --repair to add missing files", absDir)
		}
		// Repair mode: continue but only create missing files
	}

	// Check if .campaign already exists
	campaignDir := filepath.Join(absDir, config.CampaignDir)
	if _, err := os.Stat(campaignDir); err == nil {
		if !opts.Repair {
			return nil, fmt.Errorf("campaign already exists at %s", campaignDir)
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
			},
			Dry:       false,
			Overwrite: false,
		})
		if scaffoldErr != nil {
			return nil, camperrors.Wrap(scaffoldErr, "failed to create scaffold")
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
	}

	// In repair mode, preserve existing config values but add missing concepts
	if opts.Repair && existingCfg != nil {
		cfg.CreatedAt = existingCfg.CreatedAt
		cfg.Description = existingCfg.Description
		cfg.Projects = existingCfg.Projects
		// Preserve existing mission unless a new one was provided
		if opts.Mission != "" {
			cfg.Mission = opts.Mission
		} else {
			cfg.Mission = existingCfg.Mission
		}
		// Only add default concepts if none exist
		if len(existingCfg.ConceptList) > 0 {
			cfg.ConceptList = existingCfg.ConceptList
		}
	}

	if !opts.DryRun {
		if err := config.SaveCampaignConfig(ctx, absDir, cfg); err != nil {
			return nil, camperrors.Wrap(err, "failed to create campaign config")
		}
	}
	result.FilesCreated = append(result.FilesCreated, config.CampaignConfigPath(absDir))

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
`
			if err := os.WriteFile(gitignorePath, []byte(gitignoreContent), 0644); err != nil {
				return nil, camperrors.Wrap(err, "failed to create .gitignore")
			}
			result.FilesCreated = append(result.FilesCreated, ".campaign/.gitignore")
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

	// Initialize festivals directory via fest CLI if it doesn't exist
	if !opts.DryRun {
		if err := initFestivalsIfNeeded(ctx, absDir); err != nil {
			// Log the error but don't fail - user can run fest init manually
			result.Skipped = append(result.Skipped, "festivals/ (fest init failed - run manually)")
		} else {
			// Check if festivals was created
			festivalsPath := filepath.Join(absDir, "festivals")
			if _, err := os.Stat(festivalsPath); err == nil {
				result.DirsCreated = append(result.DirsCreated, festivalsPath)
			}
		}
	}

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
		reg, err := config.LoadRegistry(ctx)
		if err == nil {
			if err := reg.Register(campaignID, name, absDir, opts.Type); err == nil {
				// Ignore registry save errors - not critical
				_ = config.SaveRegistry(ctx, reg)
			}
		}
	}

	return result, nil
}

// Validate checks if the given options are valid.
func (o *InitOptions) Validate() error {
	if o.Type != "" && !o.Type.Valid() {
		return fmt.Errorf("invalid campaign type: %s", o.Type)
	}
	return nil
}

// initFestivalsIfNeeded runs `fest init` if the festivals directory doesn't exist.
// This delegates festival scaffolding to the fest CLI for proper structure.
func initFestivalsIfNeeded(ctx context.Context, dir string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	festivalsPath := filepath.Join(dir, "festivals")
	if _, err := os.Stat(festivalsPath); err == nil {
		// festivals/ already exists, skip
		return nil
	}

	// Check if fest is available
	festPath, err := exec.LookPath("fest")
	if err != nil {
		// fest not installed, skip silently - user can run fest init manually
		return nil
	}

	cmd := exec.CommandContext(ctx, festPath, "init", "--path", dir)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Don't fail the whole init if fest init fails - user can run it manually
		return fmt.Errorf("fest init failed (run manually with 'fest init'): %w (output: %s)", err, string(output))
	}

	return nil
}
