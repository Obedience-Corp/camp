package sync

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/obediencecorp/camp/internal/git"
)

// PreflightResult contains the results of all pre-flight checks.
type PreflightResult struct {
	// Passed indicates whether pre-flight checks allow sync to proceed.
	// In safe mode, uncommitted changes or unpushed commits cause failure.
	// In force mode, these are warnings only.
	Passed bool
	// UncommittedChanges lists submodules with uncommitted changes.
	UncommittedChanges []SubmoduleStatus
	// UnpushedCommits lists submodules with commits not pushed to remote.
	UnpushedCommits []SubmoduleStatus
	// URLMismatches lists submodules where .gitmodules and .git/config URLs differ.
	URLMismatches []URLMismatch
	// DetachedHEADs lists submodules in detached HEAD state.
	DetachedHEADs []DetachedHEADStatus
}

// SubmoduleStatus tracks status for a single submodule.
type SubmoduleStatus struct {
	// Path is the submodule path (e.g., "projects/camp").
	Path string
	// Details provides additional information (e.g., list of modified files).
	Details string
}

// URLMismatch represents a URL that differs between .gitmodules and .git/config.
type URLMismatch struct {
	// Submodule is the submodule path.
	Submodule string
	// DeclaredURL is the URL from .gitmodules.
	DeclaredURL string
	// ActiveURL is the URL from .git/config.
	ActiveURL string
}

// DetachedHEADStatus tracks detached HEAD state for a submodule.
type DetachedHEADStatus struct {
	// Path is the submodule path.
	Path string
	// Commit is the current HEAD commit hash.
	Commit string
	// LocalCommits is the number of commits not reachable from any branch.
	LocalCommits int
	// HasLocalWork indicates whether there are local commits that may be lost.
	HasLocalWork bool
}

// listSubmodules returns all submodule paths from .gitmodules.
func (s *Syncer) listSubmodules(ctx context.Context) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Parse submodule paths from .gitmodules using git config
	cmd := exec.CommandContext(ctx, "git", "-C", s.repoRoot,
		"config", "-f", ".gitmodules", "--get-regexp", "^submodule\\..*\\.path$")

	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			// No submodules configured
			return nil, nil
		}
		return nil, fmt.Errorf("list submodules: %w", err)
	}

	var paths []string
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: submodule.path/to/sub.path projects/sub
		parts := strings.SplitN(line, " ", 2)
		if len(parts) == 2 {
			paths = append(paths, parts[1])
		}
	}

	// Filter to only configured submodules if specified
	if len(s.options.Submodules) > 0 {
		filtered := make([]string, 0, len(s.options.Submodules))
		pathSet := make(map[string]bool)
		for _, p := range paths {
			pathSet[p] = true
		}
		for _, p := range s.options.Submodules {
			if pathSet[p] {
				filtered = append(filtered, p)
			}
		}
		paths = filtered
	}

	return paths, nil
}

// CheckUncommittedChanges detects submodules with uncommitted changes.
// Public wrapper that fetches submodule paths for standalone use.
func (s *Syncer) CheckUncommittedChanges(ctx context.Context) ([]SubmoduleStatus, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}
	return s.checkUncommittedChanges(ctx, paths)
}

// checkUncommittedChanges is the internal implementation that accepts pre-fetched paths.
func (s *Syncer) checkUncommittedChanges(ctx context.Context, paths []string) ([]SubmoduleStatus, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var results []SubmoduleStatus
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(s.repoRoot, path)
		hasChanges, err := git.HasChanges(ctx, fullPath)
		if err != nil {
			// Submodule might not be initialized
			continue
		}

		if hasChanges {
			details := s.getChangesDetails(ctx, fullPath)
			results = append(results, SubmoduleStatus{
				Path:    path,
				Details: details,
			})
		}
	}

	return results, nil
}

// getChangesDetails returns a summary of changes in a repository.
func (s *Syncer) getChangesDetails(ctx context.Context, repoPath string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "status", "--porcelain")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return ""
	}

	if len(lines) <= 3 {
		return strings.Join(lines, ", ")
	}
	return fmt.Sprintf("%d files changed", len(lines))
}

// CheckUnpushedCommits detects submodules with local commits not on remote.
// Public wrapper that fetches submodule paths for standalone use.
func (s *Syncer) CheckUnpushedCommits(ctx context.Context) ([]SubmoduleStatus, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}
	return s.checkUnpushedCommits(ctx, paths)
}

// checkUnpushedCommits is the internal implementation that accepts pre-fetched paths.
func (s *Syncer) checkUnpushedCommits(ctx context.Context, paths []string) ([]SubmoduleStatus, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var results []SubmoduleStatus
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(s.repoRoot, path)

		// Check for commits ahead of upstream
		// This will fail if no upstream is configured, which is fine
		cmd := exec.CommandContext(ctx, "git", "-C", fullPath,
			"log", "--oneline", "@{u}..HEAD")
		output, err := cmd.Output()
		if err != nil {
			// No upstream configured or other error - skip
			continue
		}

		commits := strings.TrimSpace(string(output))
		if commits != "" {
			lines := strings.Split(commits, "\n")
			results = append(results, SubmoduleStatus{
				Path:    path,
				Details: fmt.Sprintf("%d unpushed commits", len(lines)),
			})
		}
	}

	return results, nil
}

// CheckURLMismatches detects URL differences between .gitmodules and .git/config.
// Public wrapper that fetches submodule paths for standalone use.
func (s *Syncer) CheckURLMismatches(ctx context.Context) ([]URLMismatch, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}
	return s.checkURLMismatches(ctx, paths)
}

// checkURLMismatches is the internal implementation that accepts pre-fetched paths.
func (s *Syncer) checkURLMismatches(ctx context.Context, paths []string) ([]URLMismatch, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var results []URLMismatch
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		comparison, err := git.CompareURLs(ctx, s.repoRoot, path)
		if err != nil {
			// Skip submodules with URL issues
			continue
		}

		if !comparison.Match && comparison.ActiveURL != "" {
			results = append(results, URLMismatch{
				Submodule:   path,
				DeclaredURL: comparison.DeclaredURL,
				ActiveURL:   comparison.ActiveURL,
			})
		}
	}

	return results, nil
}

// CheckDetachedHEADs detects submodules in detached HEAD state.
// Public wrapper that fetches submodule paths for standalone use.
func (s *Syncer) CheckDetachedHEADs(ctx context.Context) ([]DetachedHEADStatus, error) {
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, err
	}
	return s.checkDetachedHEADs(ctx, paths)
}

// checkDetachedHEADs is the internal implementation that accepts pre-fetched paths.
func (s *Syncer) checkDetachedHEADs(ctx context.Context, paths []string) ([]DetachedHEADStatus, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	var results []DetachedHEADStatus
	for _, path := range paths {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		fullPath := filepath.Join(s.repoRoot, path)

		// Check if HEAD is detached
		cmd := exec.CommandContext(ctx, "git", "-C", fullPath, "symbolic-ref", "-q", "HEAD")
		err := cmd.Run()
		if err == nil {
			// HEAD is attached to a branch
			continue
		}

		// Get current commit
		cmd = exec.CommandContext(ctx, "git", "-C", fullPath, "rev-parse", "--short", "HEAD")
		output, err := cmd.Output()
		if err != nil {
			continue
		}
		commit := strings.TrimSpace(string(output))

		// Count local commits not on any branch
		localCommits := s.countOrphanedCommits(ctx, fullPath)

		results = append(results, DetachedHEADStatus{
			Path:         path,
			Commit:       commit,
			LocalCommits: localCommits,
			HasLocalWork: localCommits > 0,
		})
	}

	return results, nil
}

// countOrphanedCommits counts commits reachable from HEAD but not from any branch.
func (s *Syncer) countOrphanedCommits(ctx context.Context, repoPath string) int {
	// Find commits in HEAD that aren't in any branch
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath,
		"log", "--oneline", "HEAD", "--not", "--branches", "--remotes")
	output, err := cmd.Output()
	if err != nil {
		return 0
	}

	lines := strings.TrimSpace(string(output))
	if lines == "" {
		return 0
	}

	// Count lines - each line is one commit
	return len(strings.Split(lines, "\n"))
}

// RunPreflight executes all pre-flight checks.
// Fetches the submodule list once and passes it to each check, avoiding
// redundant .gitmodules parsing.
func (s *Syncer) RunPreflight(ctx context.Context) (*PreflightResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Fetch submodule paths once for all checks
	paths, err := s.listSubmodules(ctx)
	if err != nil {
		return nil, fmt.Errorf("list submodules: %w", err)
	}

	result := &PreflightResult{Passed: true}

	// Check for uncommitted changes
	uncommitted, err := s.checkUncommittedChanges(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("check uncommitted changes: %w", err)
	}
	result.UncommittedChanges = uncommitted

	// Check for unpushed commits
	unpushed, err := s.checkUnpushedCommits(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("check unpushed commits: %w", err)
	}
	result.UnpushedCommits = unpushed

	// Check for URL mismatches
	mismatches, err := s.checkURLMismatches(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("check URL mismatches: %w", err)
	}
	result.URLMismatches = mismatches

	// Check for detached HEADs
	detached, err := s.checkDetachedHEADs(ctx, paths)
	if err != nil {
		return nil, fmt.Errorf("check detached HEADs: %w", err)
	}
	result.DetachedHEADs = detached

	// Determine if checks pass based on force mode
	// In safe mode (default), uncommitted changes and unpushed commits cause failure
	if !s.options.Force {
		if len(uncommitted) > 0 || len(unpushed) > 0 {
			result.Passed = false
		}
	}
	// URL mismatches and detached HEADs are informational - sync will handle them

	return result, nil
}
