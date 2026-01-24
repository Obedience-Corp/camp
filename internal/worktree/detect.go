package worktree

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/paths"
)

// Context represents the detected worktree context.
type Context struct {
	Project      string // Project name
	WorktreeName string // Worktree directory name
	WorktreePath string // Full path to worktree root
	Branch       string // Current branch
	ProjectPath  string // Path to original project (not worktree)
}

// Detector handles worktree context detection.
type Detector struct {
	pathManager *PathManager
	resolver    *paths.Resolver
}

// NewDetector creates a new worktree detector.
func NewDetector(resolver *paths.Resolver) *Detector {
	return &Detector{
		pathManager: NewPathManager(resolver),
		resolver:    resolver,
	}
}

// DetectFromCwd detects worktree context from current working directory.
func (d *Detector) DetectFromCwd() (*Context, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	return d.DetectFromPath(cwd)
}

// DetectFromPath detects worktree context from a given path.
func (d *Detector) DetectFromPath(path string) (*Context, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}

	// Check if path is under worktrees directory
	project, name, err := d.pathManager.ParseWorktreePath(absPath)
	if err != nil {
		return nil, err
	}

	// Get the worktree root
	wtPath := d.pathManager.WorktreePath(project, name)

	// Validate it's actually a git worktree
	if !d.isGitWorktree(wtPath) {
		return nil, NewError(ErrCodeCorrupted).
			WithProject(project).
			WithWorktree(name).
			WithPath(wtPath).
			WithCause(ErrCorrupted)
	}

	// Get the branch
	branch, err := d.getCurrentBranch(wtPath)
	if err != nil {
		// Non-fatal: worktree might be in detached HEAD state
		branch = "HEAD (detached)"
	}

	// Get original project path
	projectPath := filepath.Join(d.resolver.Projects(), project)

	return &Context{
		Project:      project,
		WorktreeName: name,
		WorktreePath: wtPath,
		Branch:       branch,
		ProjectPath:  projectPath,
	}, nil
}

// isGitWorktree checks if a path is a valid git worktree.
// Worktrees have a .git file (not directory) containing gitdir path.
func (d *Detector) isGitWorktree(path string) bool {
	gitPath := filepath.Join(path, ".git")
	info, err := os.Stat(gitPath)
	if err != nil {
		return false
	}

	// Worktrees have .git as a file, not directory
	if info.IsDir() {
		return false
	}

	// Read .git file to verify it points to a gitdir
	content, err := os.ReadFile(gitPath)
	if err != nil {
		return false
	}

	return strings.HasPrefix(string(content), "gitdir:")
}

// getCurrentBranch gets the current branch of a worktree.
func (d *Detector) getCurrentBranch(wtPath string) (string, error) {
	headPath := filepath.Join(wtPath, ".git")

	// Read .git file to get gitdir
	content, err := os.ReadFile(headPath)
	if err != nil {
		return "", err
	}

	// Parse gitdir path
	gitdirLine := strings.TrimSpace(string(content))
	if !strings.HasPrefix(gitdirLine, "gitdir: ") {
		return "", ErrNotInWorktree
	}
	gitdir := strings.TrimPrefix(gitdirLine, "gitdir: ")

	// Read HEAD from gitdir
	headRef := filepath.Join(gitdir, "HEAD")
	headContent, err := os.ReadFile(headRef)
	if err != nil {
		return "", err
	}

	ref := strings.TrimSpace(string(headContent))

	// Handle symbolic ref (ref: refs/heads/branch)
	if strings.HasPrefix(ref, "ref: refs/heads/") {
		return strings.TrimPrefix(ref, "ref: refs/heads/"), nil
	}

	// Detached HEAD - return short SHA
	if len(ref) >= 7 {
		return ref[:7] + " (detached)", nil
	}

	return ref, nil
}

// IsInWorktree checks if a path is inside any worktree.
func (d *Detector) IsInWorktree(path string) bool {
	_, err := d.DetectFromPath(path)
	return err == nil
}

// IsInWorktreeCwd checks if cwd is inside any worktree.
func (d *Detector) IsInWorktreeCwd() bool {
	_, err := d.DetectFromCwd()
	return err == nil
}

// FindWorktreeRoot finds the worktree root from any path within it.
func (d *Detector) FindWorktreeRoot(path string) (string, error) {
	ctx, err := d.DetectFromPath(path)
	if err != nil {
		return "", err
	}
	return ctx.WorktreePath, nil
}
