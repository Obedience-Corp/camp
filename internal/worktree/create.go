package worktree

import (
	"context"

	"github.com/obediencecorp/camp/internal/config"
	"github.com/obediencecorp/camp/internal/paths"
)

// CreateOptions configures worktree creation.
type CreateOptions struct {
	Project     string // Project name from campaign
	Name        string // Worktree directory name
	Branch      string // Branch to checkout (default: main)
	NewBranch   bool   // Create new branch with worktree name
	TrackRemote string // Remote branch to track
}

// CreateResult contains information about created worktree.
type CreateResult struct {
	Project      string
	Name         string
	Path         string
	RelativePath string
	Branch       string
}

// Creator handles worktree creation.
type Creator struct {
	resolver    *paths.Resolver
	pathManager *PathManager
	cfg         *config.CampaignConfig
}

// NewCreator creates a Creator.
func NewCreator(resolver *paths.Resolver, cfg *config.CampaignConfig) *Creator {
	return &Creator{
		resolver:    resolver,
		pathManager: NewPathManager(resolver),
		cfg:         cfg,
	}
}

// Create creates a new worktree.
func (c *Creator) Create(ctx context.Context, opts *CreateOptions) (*CreateResult, error) {
	// 1. Validate worktree name
	if err := ValidateName(opts.Name); err != nil {
		return nil, err
	}

	// 2. Resolve project
	projectPath, err := c.resolveProject(opts.Project)
	if err != nil {
		return nil, err
	}

	// 3. Check worktree doesn't already exist
	if c.pathManager.WorktreeExists(opts.Project, opts.Name) {
		return nil, WorktreeAlreadyExists(opts.Project, opts.Name)
	}

	// 4. Ensure worktrees directory exists
	if err := c.pathManager.EnsureWorktreesDir(opts.Project); err != nil {
		return nil, NewError(ErrCodeGitFailed).
			WithProject(opts.Project).
			WithCause(err)
	}

	// 5. Create git worktree
	wtPath := c.pathManager.WorktreePath(opts.Project, opts.Name)
	git := NewGitWorktree(projectPath)

	var branch string
	if opts.TrackRemote != "" {
		// Track remote branch
		if err := git.AddTracking(ctx, wtPath, opts.TrackRemote); err != nil {
			return nil, err
		}
		branch = opts.TrackRemote
	} else if opts.NewBranch {
		// Create new branch
		if err := git.Add(ctx, wtPath, opts.Name, true); err != nil {
			return nil, err
		}
		branch = opts.Name
	} else {
		// Use existing branch
		if !git.BranchExists(ctx, opts.Branch) {
			return nil, BranchNotFoundError(opts.Project, opts.Branch)
		}
		if err := git.Add(ctx, wtPath, opts.Branch, false); err != nil {
			return nil, err
		}
		branch = opts.Branch
	}

	return &CreateResult{
		Project:      opts.Project,
		Name:         opts.Name,
		Path:         wtPath,
		RelativePath: c.pathManager.RelativeWorktreePath(opts.Project, opts.Name),
		Branch:       branch,
	}, nil
}

// resolveProject finds the project path from campaign config.
func (c *Creator) resolveProject(name string) (string, error) {
	for _, proj := range c.cfg.Projects {
		if proj.Name == name || proj.Path == "projects/"+name {
			return c.resolver.Project(name), nil
		}
	}
	return "", ProjectNotFound(name)
}
