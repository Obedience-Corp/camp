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
	Path       string
	Commit     string
	Branch     string
	IsDetached bool
	IsBare     bool
	IsLocked   bool
	Prunable   string // Reason if prunable, empty otherwise
}

// Move relocates an existing worktree while preserving its branch and dirty
// state. Git updates the administrative gitdir metadata as part of the move.
func (g *GitWorktree) Move(ctx context.Context, oldPath, newPath string) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "worktree", "move", oldPath, newPath)
	cmd.Dir = g.projectPath
	output, err := cmd.CombinedOutput()
	if err != nil {
		return GitOperationFailed(
			filepath.Base(g.projectPath),
			"worktree move",
			parseGitError(err, output),
		)
	}
	return nil
}

// CurrentBranch returns the current branch name.
func (g *GitWorktree) CurrentBranch(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = g.projectPath

	out, err := cmd.Output()
	if err != nil {
		return "", GitOperationFailed(
			filepath.Base(g.projectPath),
			"rev-parse",
			parseGitError(err, out),
		)
	}
	return strings.TrimSpace(string(out)), nil
}

// Add creates a new git worktree.
// If createBranch is true, creates a new branch with the given name based on startPoint.
// If startPoint is empty, defaults to HEAD.
func (g *GitWorktree) Add(ctx context.Context, path, branch string, createBranch bool, startPoint string) error {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	args := []string{"worktree", "add"}
	if createBranch {
		args = append(args, "-b", branch, path)
		if startPoint != "" {
			args = append(args, startPoint)
		} else {
			args = append(args, "HEAD")
		}
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

// LocalBranchExists reports whether a local branch (refs/heads/<branch>)
// exists. Unlike BranchExists it ignores remote-tracking refs, because
// 'git worktree add -b <branch>' only conflicts with a local branch of the
// same name. Creator.Create separately refuses origin/<branch> so the
// default new-branch path cannot silently fork from HEAD.
func (g *GitWorktree) LocalBranchExists(ctx context.Context, branch string) bool {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet",
		"refs/heads/"+branch)
	cmd.Dir = g.projectPath
	return cmd.Run() == nil
}

// RemoteBranchExists reports whether origin/<branch> is present as a
// remote-tracking ref (refs/remotes/origin/<branch>).
func (g *GitWorktree) RemoteBranchExists(ctx context.Context, branch string) bool {
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "git", "show-ref", "--verify", "--quiet",
		"refs/remotes/origin/"+branch)
	cmd.Dir = g.projectPath
	return cmd.Run() == nil
}

// BranchExists checks if a branch exists locally or as origin/<branch>.
func (g *GitWorktree) BranchExists(ctx context.Context, branch string) bool {
	return g.LocalBranchExists(ctx, branch) || g.RemoteBranchExists(ctx, branch)
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
				current.IsDetached = true
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
