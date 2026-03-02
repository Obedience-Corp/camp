package git

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// OrphanedGitlink represents a gitlink entry in the git index that has no
// corresponding entry in .gitmodules.
type OrphanedGitlink struct {
	// Path is the submodule path recorded in the index.
	Path string
	// Commit is the commit SHA the gitlink points to.
	Commit string
}

// ListOrphanedGitlinks compares gitlink entries in the git index (mode 160000)
// against submodule paths declared in .gitmodules. Returns entries that exist
// in the index but have no corresponding .gitmodules declaration.
func ListOrphanedGitlinks(ctx context.Context, repoRoot string) ([]OrphanedGitlink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Get all gitlink entries from the index
	indexLinks, err := listIndexGitlinks(ctx, repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "list index gitlinks")
	}

	if len(indexLinks) == 0 {
		return nil, nil
	}

	// Get declared submodule paths from .gitmodules
	declared, err := ListSubmodulePaths(ctx, repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "list submodule paths")
	}

	// Build lookup set for declared paths
	declaredSet := make(map[string]bool, len(declared))
	for _, p := range declared {
		declaredSet[p] = true
	}

	// Find orphans: in index but not in .gitmodules
	var orphans []OrphanedGitlink
	for _, link := range indexLinks {
		if !declaredSet[link.Path] {
			orphans = append(orphans, link)
		}
	}

	return orphans, nil
}

// RemoveOrphanedGitlinks removes orphaned gitlink entries from the git index
// using `git rm --cached`. Returns the paths that were successfully removed.
func RemoveOrphanedGitlinks(ctx context.Context, repoRoot string, orphans []OrphanedGitlink) ([]string, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if len(orphans) == 0 {
		return nil, nil
	}

	var removed []string
	for _, orphan := range orphans {
		if ctx.Err() != nil {
			return removed, ctx.Err()
		}

		cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "rm", "--cached", orphan.Path)
		if output, err := cmd.CombinedOutput(); err != nil {
			return removed, fmt.Errorf("%w: git rm --cached %s: %s", ErrOrphanedGitlink, orphan.Path, strings.TrimSpace(string(output)))
		}
		removed = append(removed, orphan.Path)
	}

	return removed, nil
}

// listIndexGitlinks parses `git ls-files --stage` output for mode 160000
// entries (gitlinks/submodule references).
func listIndexGitlinks(ctx context.Context, repoRoot string) ([]OrphanedGitlink, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cmd := exec.CommandContext(ctx, "git", "-C", repoRoot, "ls-files", "--stage")
	output, err := cmd.Output()
	if err != nil {
		return nil, camperrors.Wrap(err, "git ls-files --stage")
	}

	var links []OrphanedGitlink
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		// Format: <mode> <sha1> <stage>\t<path>
		// We want mode 160000 (gitlink)
		if !strings.HasPrefix(line, "160000 ") {
			continue
		}

		// Split on tab to get path
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}

		// Extract commit from the metadata portion
		metaParts := strings.Fields(parts[0])
		if len(metaParts) < 2 {
			continue
		}

		links = append(links, OrphanedGitlink{
			Path:   parts[1],
			Commit: metaParts[1],
		})
	}

	return links, scanner.Err()
}
