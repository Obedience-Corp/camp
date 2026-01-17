package scaffold

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/lancekrogers/guild-scaffold/pkg/scaffold"
	"github.com/obediencecorp/camp/internal/campaign"
	"github.com/obediencecorp/camp/internal/config"
)

// InitOptions configures the campaign initialization.
type InitOptions struct {
	// Name is the campaign name (defaults to directory name).
	Name string
	// Type is the campaign type.
	Type config.CampaignType
	// Minimal creates only essential directories.
	Minimal bool
	// FromExisting migrates an existing workspace.
	FromExisting bool
	// NoRegister skips adding to global registry.
	NoRegister bool
	// DryRun shows what would be done without creating anything.
	DryRun bool
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
}

// Init initializes a new campaign at the given directory.
func Init(ctx context.Context, dir string, opts InitOptions) (*InitResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Resolve to absolute path
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve path: %w", err)
	}

	// Check if already inside a campaign
	if _, err := campaign.Detect(ctx, absDir); err == nil {
		return nil, fmt.Errorf("already inside a campaign at %s", absDir)
	}

	// Check if .campaign already exists
	campaignDir := filepath.Join(absDir, config.CampaignDir)
	if _, err := os.Stat(campaignDir); err == nil {
		return nil, fmt.Errorf("campaign already exists at %s", campaignDir)
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

	// Generate unique campaign ID
	campaignID := uuid.New().String()

	result := &InitResult{
		ID:           campaignID,
		Name:         name,
		CampaignRoot: absDir,
	}

	// Select scaffold recipe based on minimal flag
	scaffoldPath := "campaign/scaffold.yaml"
	if opts.Minimal {
		scaffoldPath = "campaign/scaffold-minimal.yaml"
	}

	// Get expected directories and files from scaffold
	expectedDirs, expectedFiles := getExpectedPaths(absDir, opts.Minimal)

	// Check what already exists and mark as skipped
	for _, d := range expectedDirs {
		if _, err := os.Stat(d); err == nil {
			result.Skipped = append(result.Skipped, d)
		}
	}

	// Run scaffold (handles directories and template files)
	if !opts.DryRun {
		// Use guild-scaffold to create the scaffold structure
		templateFS, err := fs.Sub(CampaignScaffoldFS, "campaign/templates")
		if err != nil {
			return nil, fmt.Errorf("failed to get template sub-fs: %w", err)
		}

		_, scaffoldErr := scaffold.ScaffoldFromFS(ctx, CampaignScaffoldFS, scaffoldPath, scaffold.Options{
			TemplatesFS: templateFS,
			Dest:        absDir,
			Vars: map[string]any{
				"campaign_name": name,
				"campaign_id":   campaignID,
				"campaign_type": string(opts.Type),
			},
			Dry:       false,
			Overwrite: false,
		})
		if scaffoldErr != nil {
			return nil, fmt.Errorf("failed to create scaffold: %w", scaffoldErr)
		}
	}

	// Track what was created
	for _, d := range expectedDirs {
		if !containsPath(result.Skipped, d) {
			result.DirsCreated = append(result.DirsCreated, d)
		}
	}
	for _, f := range expectedFiles {
		result.FilesCreated = append(result.FilesCreated, f)
	}

	// Create campaign.yaml
	cfg := &config.CampaignConfig{
		ID:          campaignID,
		Name:        name,
		Type:        opts.Type,
		CreatedAt:   time.Now(),
		Description: fmt.Sprintf("Campaign: %s", name),
		Paths:       config.DefaultCampaignPaths(),
	}

	if !opts.DryRun {
		if err := config.SaveCampaignConfig(ctx, absDir, cfg); err != nil {
			return nil, fmt.Errorf("failed to create campaign config: %w", err)
		}
	}
	result.FilesCreated = append(result.FilesCreated, config.CampaignConfigPath(absDir))

	// Create AGENTS.md symlink to CLAUDE.md
	agentsPath := filepath.Join(absDir, "AGENTS.md")

	if !opts.DryRun {
		// Create symlink to CLAUDE.md (relative path)
		if _, err := os.Lstat(agentsPath); os.IsNotExist(err) {
			if err := os.Symlink("CLAUDE.md", agentsPath); err != nil {
				// Symlinks may fail on some systems, just note it
				result.Skipped = append(result.Skipped, agentsPath+" (symlink failed)")
			} else {
				result.FilesCreated = append(result.FilesCreated, agentsPath+" -> CLAUDE.md")
			}
		}
	}

	// Register in global registry
	if !opts.NoRegister && !opts.DryRun {
		reg, err := config.LoadRegistry(ctx)
		if err == nil {
			reg.Register(campaignID, name, absDir, opts.Type)
			// Ignore registry save errors - not critical
			_ = config.SaveRegistry(ctx, reg)
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

// getExpectedPaths returns the expected directories and files based on scaffold type.
func getExpectedPaths(baseDir string, minimal bool) (dirs []string, files []string) {
	// Select directories based on minimal flag
	selectedDirs := StandardDirs
	if minimal {
		selectedDirs = MinimalDirs
	}

	// Build full paths for main directories
	for _, d := range selectedDirs {
		dirs = append(dirs, filepath.Join(baseDir, d))
	}

	// Add .campaign subdirectories
	for _, d := range CampaignSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, ".campaign", d))
	}

	// Build file paths (OBEY.md files)
	for _, d := range selectedDirs {
		if d != ".campaign" {
			files = append(files, filepath.Join(baseDir, d, "OBEY.md"))
		}
	}

	// CLAUDE.md
	files = append(files, filepath.Join(baseDir, "CLAUDE.md"))

	return dirs, files
}

// containsPath checks if a string slice contains a path string.
func containsPath(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}
