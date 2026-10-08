package artifacts

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/stageguard"
)

// UntrackedFile is one file git reports as untracked that camp keeps out of
// git as artifact content.
type UntrackedFile struct {
	// Path is campaign-relative and slash-separated.
	Path string
	// Size is the file size in bytes, from lstat.
	Size int64
}

// UntrackedRoot groups one declared root's untracked artifact content.
type UntrackedRoot struct {
	// Root is the campaign-relative declared root.
	Root string
	// Files are ordered by path.
	Files []UntrackedFile
	// Bytes is the total size of Files.
	Bytes int64
}

// UntrackedContent reports, per declared root, the untracked files that a
// stage-everything commit at campRoot keeps out of git as artifact content.
// Git status lists these as untracked forever, because a mixed root is never
// gitignored; status surfaces use this to say what they really are.
//
// The rule is the commit's, not root membership. Inside a mixed root size
// decides, so a new small note beside the footage is still git's and is not
// reported here. A file only counts while the large-file guard is in auto
// mode: under block the commit refuses it and under off the commit stages it,
// so in both cases git's "untracked" is already the truth about it.
//
// The work is bounded by the declared-root list. A camp with no roots pays for
// one config read and runs no git command.
func UntrackedContent(ctx context.Context, campRoot string) ([]UntrackedRoot, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	cfg, err := Load(campRoot)
	if err != nil {
		return nil, err
	}
	roots := presentRoots(campRoot, cfg)
	if len(roots) == 0 {
		return nil, nil
	}

	limits, err := stageguard.ResolveLimits(ctx, campRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "resolve staging guard limits")
	}
	if limits.LargeFiles != stageguard.ModeAuto {
		return nil, nil
	}

	violations, err := stageguard.UntrackedOverThreshold(ctx, campRoot, limits, roots)
	if err != nil {
		return nil, camperrors.Wrap(err, "find untracked artifact content")
	}
	return groupByRoot(roots, violations), nil
}

// presentRoots returns the normalized declared roots that exist on this
// machine. A root missing here has no files to report, and the missing-root
// notice already covers it.
func presentRoots(campRoot string, cfg *File) []string {
	if cfg == nil {
		return nil
	}
	roots := make([]string, 0, len(cfg.Roots))
	for _, r := range cfg.Roots {
		root := NormalizeRootPath(r.Path)
		if root == "" || root == "." {
			continue
		}
		if info, err := os.Stat(filepath.Join(campRoot, filepath.FromSlash(root))); err != nil || !info.IsDir() {
			continue
		}
		roots = append(roots, root)
	}
	return roots
}

// groupByRoot files each violation under the deepest root that contains it, so
// a root declared inside another root keeps its own files.
func groupByRoot(roots []string, violations []stageguard.GuardViolation) []UntrackedRoot {
	byRoot := make(map[string]*UntrackedRoot)
	for _, v := range violations {
		root, ok := deepestRoot(roots, v.Path)
		if !ok {
			continue
		}
		group := byRoot[root]
		if group == nil {
			group = &UntrackedRoot{Root: root}
			byRoot[root] = group
		}
		group.Files = append(group.Files, UntrackedFile{Path: v.Path, Size: v.Size})
		group.Bytes += v.Size
	}

	grouped := make([]UntrackedRoot, 0, len(byRoot))
	for _, group := range byRoot {
		sort.Slice(group.Files, func(i, j int) bool { return group.Files[i].Path < group.Files[j].Path })
		grouped = append(grouped, *group)
	}
	sort.Slice(grouped, func(i, j int) bool { return grouped[i].Root < grouped[j].Root })
	return grouped
}

func deepestRoot(roots []string, rel string) (string, bool) {
	best := ""
	for _, root := range roots {
		if (rel == root || strings.HasPrefix(rel, root+"/")) && len(root) > len(best) {
			best = root
		}
	}
	return best, best != ""
}

// UntrackedPaths flattens grouped content into its file paths.
func UntrackedPaths(roots []UntrackedRoot) []string {
	var paths []string
	for _, r := range roots {
		for _, f := range r.Files {
			paths = append(paths, f.Path)
		}
	}
	return paths
}
