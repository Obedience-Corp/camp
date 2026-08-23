package worktree

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
)

// CreateOptions configures worktree creation.
type CreateOptions struct {
	Project     string // Project name from campaign
	ProjectPath string // Resolved git repository path, when already known
	Name        string // Worktree directory name
	Branch      string // Branch to checkout or create
	NewBranch   bool   // Create new branch with worktree name
	StartPoint  string // Base branch/commit for new branch (defaults to current branch)
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
	projectPath := opts.ProjectPath
	if projectPath == "" {
		var err error
		projectPath, err = c.resolveProject(opts.Project)
		if err != nil {
			return nil, err
		}
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

	// 5. Guard against cross-project symlinks.
	//
	// If the worktree holder (projects/worktrees/<project>) is itself a
	// symlink into another project's working tree, the canonical worktree
	// path lands inside that other project. Git then registers the worktree
	// at the resolved path, and camp p commit — which relies on
	// os.Getwd() (already symlink-resolved on macOS) — commits against the
	// wrong project. Refuse early so the situation never arises.
	wtPath := c.pathManager.WorktreePath(opts.Project, opts.Name)
	if err := c.guardCrossProjectSymlink(opts.Project, opts.Name, wtPath); err != nil {
		return nil, err
	}

	// 6. Create git worktree
	git := NewGitWorktree(projectPath)

	var branch string
	if opts.TrackRemote != "" {
		// Track remote branch
		if err := git.AddTracking(ctx, wtPath, opts.TrackRemote); err != nil {
			return nil, err
		}
		branch = opts.TrackRemote
	} else if opts.NewBranch {
		// Create new branch based on start point (or current branch if not specified)
		branchName := opts.Branch
		if branchName == "" {
			branchName = opts.Name
		}
		// Detect leftover local branches and same-named origin branches
		// before git does. git worktree add -b only refuses the local case;
		// origin/<name> would silently fork from HEAD.
		if err := newBranchConflict(opts.Project, branchName,
			git.LocalBranchExists(ctx, branchName),
			git.RemoteBranchExists(ctx, branchName)); err != nil {
			return nil, err
		}
		if err := git.Add(ctx, wtPath, branchName, true, opts.StartPoint); err != nil {
			return nil, err
		}
		branch = branchName
	} else {
		// Use existing branch
		if !git.BranchExists(ctx, opts.Branch) {
			return nil, BranchNotFoundError(opts.Project, opts.Branch)
		}
		if err := git.Add(ctx, wtPath, opts.Branch, false, ""); err != nil {
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

// newBranchConflict returns an error when creating a new local branch named
// branch would collide with an existing local branch or silently shadow
// origin/branch. Local collisions win because git worktree add -b already
// refuses them.
func newBranchConflict(project, branch string, localExists, remoteExists bool) error {
	if localExists {
		return BranchAlreadyExists(project, branch)
	}
	if remoteExists {
		return RemoteBranchExistsError(project, branch)
	}
	return nil
}

// guardCrossProjectSymlink refuses a worktree whose destination resolves
// (via symlinks) into a different registered project's working tree.
//
// The worktree holder path is logical (e.g.
// <root>/projects/worktrees/<project>/<name>). When <project> is a symlink
// into another project's directory, the resolved path falls inside that other
// project, and camp p commit would detect the wrong project from the
// worktree's cwd. This guard catches the situation at creation time.
func (c *Creator) guardCrossProjectSymlink(project, worktreeName, logicalPath string) error {
	// Resolve the worktree's parent directory (which already exists after
	// EnsureWorktreesDir). The worktree path itself does not exist yet.
	parent := filepath.Dir(logicalPath)
	resolvedParent, err := filepath.EvalSymlinks(parent)
	if err != nil {
		// If the parent does not resolve, there is no symlink to cross a
		// project boundary. Let git create the worktree.
		return nil
	}
	resolvedPath := filepath.Join(resolvedParent, filepath.Base(logicalPath))

	// Check whether the resolved path falls inside any other registered
	// project's directory. The requesting project's own source directory
	// is excluded — a worktree that resolves into its own project is fine
	// (e.g. a linked project whose worktrees dir is under the project).
	for _, proj := range c.cfg.Projects {
		if proj.Name == project {
			continue
		}
		projPath := c.resolver.Project(proj.Name)
		resolvedProj, err := filepath.EvalSymlinks(projPath)
		if err != nil {
			continue
		}
		if isPathUnder(resolvedPath, resolvedProj) {
			return CrossProjectSymlinkError(project, worktreeName, logicalPath, resolvedPath, proj.Name)
		}
	}

	return nil
}

// isPathUnder reports whether child is the same as or inside parent.
func isPathUnder(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	child = filepath.Clean(child)
	parent = filepath.Clean(parent)
	if child == parent {
		return true
	}
	return strings.HasPrefix(child, parent+string(filepath.Separator))
}

// resolveProject finds the project path from campaign config or filesystem.
func (c *Creator) resolveProject(name string) (string, error) {
	if name == "" {
		return "", ProjectNotFound(name)
	}

	// First check configured projects
	for _, proj := range c.cfg.Projects {
		if proj.Name == name || proj.Path == "projects/"+name {
			return c.resolver.Project(name), nil
		}
	}

	// Fall back to filesystem detection - check if project directory exists
	projectPath := c.resolver.Project(name)
	if info, err := os.Stat(projectPath); err == nil && info.IsDir() {
		return projectPath, nil
	}

	return "", ProjectNotFound(name)
}
