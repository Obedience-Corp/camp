package jobs

import (
	"path/filepath"
	"strings"
)

// rootLaneSlug is the lane for the campaign root.
//
// It is percent-encoded rather than a word like "root" because a word could
// collide with a real repository: a submodule at campaign-relative path "root"
// would slug to the same directory and silently share a lane with the campaign
// itself, serializing unrelated work and confusing every listing. Percent
// encoding cannot collide, since a literal "%" in a path is escaped first.
const rootLaneSlug = "%2E"

// LaneSlug maps a campaign-relative repo path to a lane directory name.
//
// The mapping must be deterministic and collision-free: two repos sharing a
// lane would serialize against each other for no reason, and worse, their
// sequence numbers would interleave so `camp jobs` could not say which job
// belongs to which repo.
//
// The encoding is the one internal/artifacts already uses for peer snapshot
// filenames: escape "%" first, then "/". Escaping "%" first is what makes the
// mapping injective, since otherwise a path containing a literal "%2F" would
// decode ambiguously.
func LaneSlug(repo string) string {
	normalized := normalizeRepo(repo)
	if normalized == "" || normalized == "." {
		return rootLaneSlug
	}
	slug := strings.ReplaceAll(normalized, "%", "%25")
	slug = strings.ReplaceAll(slug, "/", "%2F")
	return slug
}

// normalizeRepo cleans a campaign-relative repo path to slash-separated form
// with no leading "./" or trailing "/", so the same repo spelled differently
// lands in one lane.
func normalizeRepo(repo string) string {
	trimmed := strings.TrimSpace(repo)
	if trimmed == "" {
		return "."
	}
	return strings.Trim(filepath.ToSlash(filepath.Clean(trimmed)), "/")
}

// RepoFromLaneSlug reverses LaneSlug, for rendering a lane back to the user.
func RepoFromLaneSlug(slug string) string {
	if slug == rootLaneSlug {
		return "."
	}
	repo := strings.ReplaceAll(slug, "%2F", "/")
	return strings.ReplaceAll(repo, "%25", "%")
}
