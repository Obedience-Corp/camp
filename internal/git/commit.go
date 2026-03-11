package git

import (
	"context"
	"errors"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// CommitOptions configures the commit operation.
type CommitOptions struct {
	Message    string
	Amend      bool
	AllowEmpty bool
	Author     string   // Optional: "Name <email>"
	Only       []string // If set, commit only these paths (git commit --only -- <paths>)
}

// Validate checks if options are valid.
func (o *CommitOptions) Validate() error {
	if o.Message == "" && !o.Amend {
		return ErrCommitMessageRequired
	}
	return nil
}

// Commit creates a git commit with automatic lock handling.
func Commit(ctx context.Context, repoPath string, opts *CommitOptions) error {
	if opts == nil {
		return ErrCommitOptionsRequired
	}
	if err := opts.Validate(); err != nil {
		return err
	}

	cfg := DefaultRetryConfig()
	cfg.OperationName = "commit"

	return WithLockRetry(ctx, repoPath, cfg, func() error {
		return executeCommit(ctx, repoPath, opts)
	})
}

// executeCommit runs the actual git commit command.
func executeCommit(ctx context.Context, repoPath string, opts *CommitOptions) error {
	args := []string{"-C", repoPath, "commit"}

	if opts.Amend {
		args = append(args, "--amend")
	}
	if opts.AllowEmpty {
		args = append(args, "--allow-empty")
	}
	if opts.Author != "" {
		args = append(args, "--author", opts.Author)
	}
	if opts.Message != "" {
		args = append(args, "-m", opts.Message)
	}
	if len(opts.Only) > 0 {
		args = append(args, "--only", "--")
		args = append(args, opts.Only...)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())

		switch errType {
		case GitErrorNoChanges:
			return ErrNoChanges
		case GitErrorLock:
			return &LockError{
				Path: "index.lock", // Will be found by cleanup
				Err:  err,
			}
		default:
			return camperrors.NewGit("commit", "", errType.String(), strings.TrimSpace(string(output)), err)
		}
	}

	return nil
}

// isLockError checks if an error is a lock-related error.
func isLockError(err error) bool {
	var lockErr *LockError
	return errors.As(err, &lockErr)
}

// CommitAll stages all changes and commits with the given message.
func CommitAll(ctx context.Context, repoPath, message string) error {
	// Stage all changes first
	if err := StageAll(ctx, repoPath); err != nil {
		return err
	}

	// Check if there's anything to commit
	hasChanges, err := HasStagedChanges(ctx, repoPath)
	if err != nil {
		return err
	}
	if !hasChanges {
		return ErrNoChanges
	}

	// Commit
	return Commit(ctx, repoPath, &CommitOptions{Message: message})
}

// Stage adds files to the git index (staging area) with automatic lock handling.
// If files is empty, stages all changes (git add .).
func Stage(ctx context.Context, repoPath string, files []string) error {
	cfg := DefaultRetryConfig()
	cfg.OperationName = "stage"

	return WithLockRetry(ctx, repoPath, cfg, func() error {
		return executeStage(ctx, repoPath, files)
	})
}

// executeStage runs the actual git add command.
func executeStage(ctx context.Context, repoPath string, files []string) error {
	var args []string

	// Use -C to run git in the specified directory
	args = append(args, "-C", repoPath, "add")

	if len(files) == 0 {
		// Stage all changes
		args = append(args, ".")
	} else {
		// Stage specific files — use "--" to prevent filenames from being
		// interpreted as options (e.g. a file named "-abc").
		// Skip if caller already provided "--" (e.g. StageAllExcluding).
		if len(files) == 0 || files[0] != "--" {
			args = append(args, "--")
		}
		args = append(args, files...)
	}

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())

		// Return LockError for lock issues so retry logic can handle it
		if errType == GitErrorLock {
			return &LockError{
				Path: "index.lock",
				Err:  err,
			}
		}

		return camperrors.NewGit("add", "", errType.String(), strings.TrimSpace(string(output)), err)
	}

	return nil
}

// StageAll is a convenience function to stage all changes.
func StageAll(ctx context.Context, repoPath string) error {
	return Stage(ctx, repoPath, nil)
}

// StageTrackedChanges stages modifications and deletions of already-tracked files
// under the given paths, without adding new untracked files (git add -u).
func StageTrackedChanges(ctx context.Context, repoPath string, paths ...string) error {
	if len(paths) == 0 {
		return nil
	}
	cfg := DefaultRetryConfig()
	cfg.OperationName = "stage-tracked"

	return WithLockRetry(ctx, repoPath, cfg, func() error {
		args := []string{"-C", repoPath, "add", "-u", "--"}
		args = append(args, paths...)

		cmd := exec.CommandContext(ctx, "git", args...)
		output, err := cmd.CombinedOutput()
		if err != nil {
			errType := ClassifyGitError(string(output), cmd.ProcessState.ExitCode())
			if errType == GitErrorLock {
				return &LockError{Path: "index.lock", Err: err}
			}
			return camperrors.NewGit("add -u", "", errType.String(), strings.TrimSpace(string(output)), err)
		}
		return nil
	})
}

// FilterTracked returns only the paths from the input that git currently tracks.
// For directories, a path is considered tracked if any file under it is in the index.
// Useful for filtering commit scopes to avoid "pathspec did not match" errors.
func FilterTracked(ctx context.Context, repoPath string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := []string{"-C", repoPath, "ls-files", "--"}
	args = append(args, paths...)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.NewGit("ls-files", "", "", strings.TrimSpace(string(output)), err)
	}

	raw := strings.TrimSpace(string(output))
	if raw == "" {
		return nil, nil
	}

	// Build a set of tracked file paths returned by git
	trackedFiles := strings.Split(raw, "\n")
	trackedSet := make(map[string]bool, len(trackedFiles))
	for _, f := range trackedFiles {
		trackedSet[f] = true
	}

	var result []string
	for _, p := range paths {
		// Exact match (file path)
		if trackedSet[p] {
			result = append(result, p)
			continue
		}
		// Directory match: check if any tracked file has this prefix
		prefix := p + "/"
		for t := range trackedSet {
			if strings.HasPrefix(t, prefix) {
				result = append(result, p)
				break
			}
		}
	}
	return result, nil
}

// ExpandTrackedPaths resolves the given pathspecs to actual staged file paths
// currently present in the index. This expands directories to the staged file
// paths they affect so they can be safely passed to `git commit --only`.
// Staged renames are returned as both source and destination paths so a scoped
// commit does not drop the source-side deletion.
func ExpandTrackedPaths(ctx context.Context, repoPath string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, nil
	}

	args := []string{"-C", repoPath, "diff", "--cached", "--name-status", "-z", "--"}
	args = append(args, paths...)

	cmd := exec.CommandContext(ctx, "git", args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.NewGit("diff --cached", "", "", strings.TrimSpace(string(output)), err)
	}

	if len(output) == 0 {
		return nil, nil
	}

	fields := strings.Split(string(output), "\x00")
	seen := make(map[string]struct{}, len(fields))
	result := make([]string, 0, len(fields))
	addPath := func(path string) {
		if path == "" {
			return
		}
		if _, ok := seen[path]; ok {
			return
		}
		seen[path] = struct{}{}
		result = append(result, path)
	}

	for i := 0; i < len(fields); {
		status := fields[i]
		i++
		if status == "" {
			continue
		}

		switch status[0] {
		case 'R', 'C':
			if i+1 >= len(fields) {
				return nil, camperrors.NewGit("diff --cached", "", "", "malformed rename/copy status output", nil)
			}
			addPath(fields[i])
			addPath(fields[i+1])
			i += 2
		default:
			if i >= len(fields) {
				return nil, camperrors.NewGit("diff --cached", "", "", "malformed diff status output", nil)
			}
			addPath(fields[i])
			i++
		}
	}
	return result, nil
}

// StageFiles stages specific files.
func StageFiles(ctx context.Context, repoPath string, files ...string) error {
	if len(files) == 0 {
		return ErrNoFilesSpecified
	}
	return Stage(ctx, repoPath, files)
}

// StageAllExcluding stages all changes except paths matching the given exclusions.
// Uses git pathspec exclusion (`:!path`) for atomic single-operation staging.
func StageAllExcluding(ctx context.Context, repoPath string, excludePaths []string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if len(excludePaths) == 0 {
		return StageAll(ctx, repoPath)
	}

	files := []string{"--", "."}
	for _, p := range excludePaths {
		files = append(files, ":!"+p)
	}
	return Stage(ctx, repoPath, files)
}

// HasStagedChanges checks if there are any staged changes ready to commit.
func HasStagedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--cached", "--quiet")
	err := cmd.Run()

	if err != nil {
		// Exit code 1 means there are differences (staged changes)
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, camperrors.NewGit("diff --cached", "", "", "", err)
	}

	// Exit code 0 means no differences (nothing staged)
	return false, nil
}

// HasUnstagedChanges checks if there are any unstaged changes.
func HasUnstagedChanges(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "diff", "--quiet")
	err := cmd.Run()

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				return true, nil
			}
		}
		return false, camperrors.NewGit("diff", "", "", "", err)
	}

	return false, nil
}

// HasUntrackedFiles checks if there are any untracked files.
func HasUntrackedFiles(ctx context.Context, repoPath string) (bool, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "ls-files", "--others", "--exclude-standard")
	output, err := cmd.Output()

	if err != nil {
		return false, camperrors.NewGit("ls-files", "", "", "", err)
	}

	return len(strings.TrimSpace(string(output))) > 0, nil
}

// HasChanges checks if there are any staged, unstaged, or untracked changes.
func HasChanges(ctx context.Context, repoPath string) (bool, error) {
	staged, err := HasStagedChanges(ctx, repoPath)
	if err != nil {
		return false, err
	}
	if staged {
		return true, nil
	}

	unstaged, err := HasUnstagedChanges(ctx, repoPath)
	if err != nil {
		return false, err
	}
	if unstaged {
		return true, nil
	}

	untracked, err := HasUntrackedFiles(ctx, repoPath)
	if err != nil {
		return false, err
	}

	return untracked, nil
}
