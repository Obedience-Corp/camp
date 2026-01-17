package scaffold

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

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

	result := &InitResult{
		CampaignRoot: absDir,
	}

	// Select directories to create
	dirs := StandardDirs
	if opts.Minimal {
		dirs = MinimalDirs
	}

	// Create directories
	for _, d := range dirs {
		path := filepath.Join(absDir, d)
		if opts.DryRun {
			result.DirsCreated = append(result.DirsCreated, path)
			continue
		}

		if _, err := os.Stat(path); err == nil {
			result.Skipped = append(result.Skipped, path)
			continue
		}

		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", path, err)
		}
		result.DirsCreated = append(result.DirsCreated, path)
	}

	// Create .campaign subdirectories
	for _, d := range CampaignSubdirs {
		path := filepath.Join(absDir, config.CampaignDir, d)
		if opts.DryRun {
			result.DirsCreated = append(result.DirsCreated, path)
			continue
		}

		if _, err := os.Stat(path); err == nil {
			result.Skipped = append(result.Skipped, path)
			continue
		}

		if err := os.MkdirAll(path, 0755); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", path, err)
		}
		result.DirsCreated = append(result.DirsCreated, path)
	}

	// Create campaign.yaml
	cfg := &config.CampaignConfig{
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
		result.FilesCreated = append(result.FilesCreated, config.CampaignConfigPath(absDir))
	}

	// Create OBEY.md files for each directory
	if !opts.DryRun {
		if err := CreateObeyFiles(ctx, absDir, opts.Minimal); err != nil {
			return nil, fmt.Errorf("failed to create OBEY.md files: %w", err)
		}
		// Track OBEY.md files created (one for each non-.campaign directory)
		for _, d := range dirs {
			if d != ".campaign" {
				if _, ok := ObeyContent[d]; ok {
					result.FilesCreated = append(result.FilesCreated, filepath.Join(absDir, d, "OBEY.md"))
				}
			}
		}
	}

	// Create CLAUDE.md and AGENTS.md symlink
	claudePath := filepath.Join(absDir, "CLAUDE.md")
	agentsPath := filepath.Join(absDir, "AGENTS.md")

	if !opts.DryRun {
		// Create CLAUDE.md using the template
		if err := CreateClaudeMD(ctx, absDir, name); err != nil {
			return nil, fmt.Errorf("failed to create CLAUDE.md: %w", err)
		}
		if _, err := os.Stat(claudePath); err == nil {
			result.FilesCreated = append(result.FilesCreated, claudePath)
		}

		// Create AGENTS.md symlink to CLAUDE.md
		if err := CreateAgentsMDSymlink(ctx, absDir); err != nil {
			// Symlinks may fail on some systems, just note it
			result.Skipped = append(result.Skipped, agentsPath+" (symlink failed)")
		} else {
			if _, err := os.Lstat(agentsPath); err == nil {
				result.FilesCreated = append(result.FilesCreated, agentsPath+" -> CLAUDE.md")
			}
		}
	}

	// Register in global registry
	if !opts.NoRegister && !opts.DryRun {
		reg, err := config.LoadRegistry(ctx)
		if err == nil {
			reg.Register(name, absDir, opts.Type)
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
