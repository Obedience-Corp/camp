package intent

import (
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/pathutil"
)

// displayPath returns a user-facing path for CLI success messages.
// When path is under campaignRoot, prefer a campaign-relative form so fixtures
// and tapes do not leak absolute host paths (e.g. /private/tmp/...).
// Falls back to the original path if relativization fails.
func displayPath(campaignRoot, path string) string {
	if path == "" {
		return path
	}
	if campaignRoot == "" {
		return path
	}
	// Canonicalize root so Rel matches pathutil.RelativeToRoot's symlink-resolved paths
	// (e.g. macOS /var/folders → /private/var/folders).
	resolvedRoot, err := pathutil.ResolveRoot(campaignRoot)
	if err != nil || resolvedRoot == "" {
		resolvedRoot = campaignRoot
	}
	rel, err := pathutil.RelativeToRoot(resolvedRoot, path)
	if err != nil || rel == "" || rel == "." {
		return path
	}
	// Reject escapes outside the campaign (Rel can return ../...).
	if strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return path
	}
	return filepath.ToSlash(rel)
}
