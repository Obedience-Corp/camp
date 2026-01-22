package scaffold

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
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
		return nil, fmt.Errorf("failed to resolve path: %w", err)
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

	// Generate unique campaign ID
	campaignID := uuid.New().String()

	result := &InitResult{
		ID:           campaignID,
		Name:         name,
		CampaignRoot: absDir,
	}

	// Scaffold path
	scaffoldPath := "campaign/scaffold.yaml"

	// Get expected directories and files from scaffold
	expectedDirs, expectedFiles := getExpectedPaths(absDir)

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
		Shortcuts:   config.DefaultNavigationShortcuts(),
	}

	if !opts.DryRun {
		if err := config.SaveCampaignConfig(ctx, absDir, cfg); err != nil {
			return nil, fmt.Errorf("failed to create campaign config: %w", err)
		}
	}
	result.FilesCreated = append(result.FilesCreated, config.CampaignConfigPath(absDir))

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
				return nil, fmt.Errorf("failed to create .gitignore: %w", err)
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

// getExpectedPaths returns the expected directories and files.
func getExpectedPaths(baseDir string) (dirs []string, files []string) {
	// Build full paths for main directories
	for _, d := range StandardDirs {
		dirs = append(dirs, filepath.Join(baseDir, d))
	}

	// Add .campaign subdirectories
	for _, d := range CampaignSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, ".campaign", d))
	}

	// Add projects subdirectories (worktrees)
	for _, d := range ProjectsSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, "projects", d))
	}

	// Add dungeon subdirectories
	for _, d := range DungeonSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, "dungeon", d))
	}

	// Add workflow subdirectories
	for _, d := range WorkflowSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, "workflow", d))
	}

	// Add intents subdirectories (under workflow)
	for _, d := range IntentsSubdirs {
		dirs = append(dirs, filepath.Join(baseDir, "workflow", "intents", d))
	}

	// Build file paths (OBEY.md files for main dirs)
	for _, d := range StandardDirs {
		if d != ".campaign" {
			files = append(files, filepath.Join(baseDir, d, "OBEY.md"))
		}
	}

	// OBEY.md files for subdirectories
	for _, d := range ProjectsSubdirs {
		files = append(files, filepath.Join(baseDir, "projects", d, "OBEY.md"))
	}
	for _, d := range WorkflowSubdirs {
		files = append(files, filepath.Join(baseDir, "workflow", d, "OBEY.md"))
	}

	// AGENTS.md (source of truth) and README.md
	files = append(files, filepath.Join(baseDir, "AGENTS.md"))
	files = append(files, filepath.Join(baseDir, "README.md"))

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
