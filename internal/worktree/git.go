package worktree

import (
	"bufio"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GitWorktree provides git worktree command wrappers.
type GitWorktree struct {
	projectPath string
	timeout     time.Duration
}

// NewGitWorktree creates a GitWorktree for a project.
func NewGitWorktree(projectPath string) *GitWorktree {
	return &GitWorktree{
		projectPath: projectPath,
		timeout:     30 * time.Second,
	}
}

// WithTimeout sets the command timeout.
func (g *GitWorktree) WithTimeout(d time.Duration) *GitWorktree {
	g.timeout = d
	return g
}

// GitWorktreeEntry represents a worktree from git worktree list.
type GitWorktreeEntry struct {
	Path     string
	Commit   string
	Branch   string
	IsBare   bool
	IsLocked bool
	Prunable string // Reason if prunable, empty otherwise
}

// Add creates a new git worktree.
func (g *GitWorktree) Add(ctx context.Context, path, branch string, createBranch bool) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	args := []string{"worktree", "add"}
	if createBranch {
		args = append(args, "-b", branch, path, "HEAD")
	} else {
		args = append(args, path, branch)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree add",
			parseGitError(err, output),
		)
	}

	return nil
}

// AddTracking creates a worktree tracking a remote branch.
func (g *GitWorktree) AddTracking(ctx context.Context, path, remoteBranch string) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Extract branch name from remote (e.g., origin/feature -> feature)
	parts := strings.SplitN(remoteBranch, "/", 2)
	localBranch := remoteBranch
	if len(parts) == 2 {
		localBranch = parts[1]
	}

	args := []string{"worktree", "add", "--track", "-b", localBranch, path, remoteBranch}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree add --track",
			parseGitError(err, output),
		)
	}

	return nil
}

// List returns all worktrees for the project.
func (g *GitWorktree) List(ctx context.Context) ([]GitWorktreeEntry, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "worktree", "list", "--porcelain")
	cmd.Dir = g.projectPath

	output, err := cmd.Output()
	if err != nil {
		return nil, GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree list",
			err,
		)
	}

	return parseWorktreeList(string(output)), nil
}

// Remove removes a worktree.
func (g *GitWorktree) Remove(ctx context.Context, path string, force bool) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	args := []string{"worktree", "remove"}
	if force {
		args = append(args, "--force")
	}
	args = append(args, path)

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree remove",
			parseGitError(err, output),
		)
	}

	return nil
}

// Prune removes stale worktree references.
func (g *GitWorktree) Prune(ctx context.Context, dryRun bool) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	args := []string{"worktree", "prune"}
	if dryRun {
		args = append(args, "--dry-run", "-v")
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = g.projectPath

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree prune",
			parseGitError(err, output),
		)
	}

	// Parse pruned paths from output
	var pruned []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "Removing") {
			pruned = append(pruned, line)
		}
	}

	return pruned, nil
}

// BranchExists checks if a branch exists.
func (g *GitWorktree) BranchExists(ctx context.Context, branch string) bool {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Check local branches
	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet",
		"refs/heads/"+branch)
	cmd.Dir = g.projectPath
	if cmd.Run() == nil {
		return true
	}

	// Check remote branches
	cmd = exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet",
		"refs/remotes/origin/"+branch)
	cmd.Dir = g.projectPath
	return cmd.Run() == nil
}

// parseWorktreeList parses git worktree list --porcelain output.
func parseWorktreeList(output string) []GitWorktreeEntry {
	var entries []GitWorktreeEntry
	var current *GitWorktreeEntry

	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			if current != nil {
				entries = append(entries, *current)
				current = nil
			}
			continue
		}

		if strings.HasPrefix(line, "worktree ") {
			current = &GitWorktreeEntry{
				Path: strings.TrimPrefix(line, "worktree "),
			}
		} else if current != nil {
			switch {
			case strings.HasPrefix(line, "HEAD "):
				current.Commit = strings.TrimPrefix(line, "HEAD ")
			case strings.HasPrefix(line, "branch refs/heads/"):
				current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
			case line == "bare":
				current.IsBare = true
			case line == "locked":
				current.IsLocked = true
			case strings.HasPrefix(line, "prunable "):
				current.Prunable = strings.TrimPrefix(line, "prunable ")
			case line == "detached":
				current.Branch = "HEAD (detached)"
			}
		}
	}

	// Don't forget last entry
	if current != nil {
		entries = append(entries, *current)
	}

	return entries
}

// parseGitError extracts meaningful error from git output.
func parseGitError(err error, output []byte) error {
	if len(output) > 0 {
		return &gitError{
			cause:  err,
			output: strings.TrimSpace(string(output)),
		}
	}
	return err
}

type gitError struct {
	cause  error
	output string
}

func (e *gitError) Error() string {
	return e.output
}

func (e *gitError) Unwrap() error {
	return e.cause
}
