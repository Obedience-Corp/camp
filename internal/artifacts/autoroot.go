package artifacts

import (
	"os"
	"path/filepath"
	"strings"
)

// RefusalReason says why camp declined to choose an artifact root for a file.
// It is data, not a message: the CLI renders each reason with the ask that
// fits it, and --json reports the reason verbatim.
type RefusalReason string

const (
	// RefusalNone means a root was resolved.
	RefusalNone RefusalReason = ""
	// RefusalCampaignRoot: the file sits directly at the campaign root, so the
	// only candidate parent is the campaign itself. Declaring that would turn
	// the whole campaign into an artifact root.
	RefusalCampaignRoot RefusalReason = "campaign_root"
	// RefusalCampaignState: the path is inside .campaign/, camp's own state
	// tree. A regenerated graph export escaping its gitignore must not turn a
	// subdirectory of camp's state into a user artifact root.
	RefusalCampaignState RefusalReason = "campaign_state"
	// RefusalRepoRoot: the candidate is a git repository root, including a
	// submodule's. Artifact roots are campaign-relative, so one here would
	// cross a repo boundary.
	RefusalRepoRoot RefusalReason = "repo_root"
	// RefusalEscapesCampaign: the path is not inside the campaign at all.
	RefusalEscapesCampaign RefusalReason = "escapes_campaign"
)

// AutoRoot is the outcome of choosing a root for one over-threshold file.
type AutoRoot struct {
	// Path is the campaign-relative root to declare, empty when refused.
	Path string
	// Refusal says why no root was chosen. RefusalNone means Path is usable.
	Refusal RefusalReason
	// Existing is true when Path is already declared, or lies inside an
	// already-declared root. Camp reuses it rather than declaring a sibling.
	Existing bool
}

// Declared reports whether a new declaration is needed.
func (r AutoRoot) Declared() bool {
	return r.Refusal == RefusalNone && !r.Existing
}

// ResolveAutoRoot chooses the artifact root to declare for an over-threshold
// file, or refuses.
//
// The default is the file's parent directory. That is safe specifically
// because tracked files inside a declared root stay git's: declaring
// videos/my-video/ does not disturb the script.md sitting beside the footage,
// so camp can make the declaration without inferring intent.
//
// Refusal is per file. The caller excludes that one file, reports the ask, and
// commits everything else; aborting the whole commit here would reintroduce
// the decision point this package exists to remove, for a case that is not rare.
func ResolveAutoRoot(cfg *File, campRoot, fileRel string) AutoRoot {
	normalized := NormalizeRootPath(fileRel)
	if normalized == "" || normalized == "." {
		return AutoRoot{Refusal: RefusalEscapesCampaign}
	}
	if !filepath.IsLocal(filepath.FromSlash(normalized)) {
		return AutoRoot{Refusal: RefusalEscapesCampaign}
	}
	if isCampaignState(normalized) {
		return AutoRoot{Refusal: RefusalCampaignState}
	}

	parent := parentDir(normalized)
	if parent == "" {
		// The file sits at the campaign root; its only candidate parent is the
		// campaign itself.
		return AutoRoot{Refusal: RefusalCampaignRoot}
	}
	if isCampaignState(parent) {
		return AutoRoot{Refusal: RefusalCampaignState}
	}

	// Reuse a root that already covers this file rather than declaring a
	// nested sibling inside it.
	if existing, ok := enclosingRoot(cfg, normalized); ok {
		return AutoRoot{Path: existing, Existing: true}
	}

	if isRepoRoot(campRoot, parent) {
		return AutoRoot{Refusal: RefusalRepoRoot}
	}
	return AutoRoot{Path: parent}
}

// parentDir returns the slash-separated parent of a campaign-relative path, or
// "" when the path sits directly at the campaign root.
func parentDir(rel string) string {
	idx := strings.LastIndex(rel, "/")
	if idx <= 0 {
		return ""
	}
	return rel[:idx]
}

// isCampaignState reports whether a campaign-relative path is .campaign or
// lives beneath it. Case-insensitive to match ValidateRootPath, since a
// case-insensitive filesystem would otherwise let ".Campaign/" through.
func isCampaignState(rel string) bool {
	return strings.EqualFold(rel, CampaignStateDir) ||
		hasCaseInsensitivePrefix(rel, CampaignStateDir+"/")
}

// CampaignStateDir is camp's own state tree, never a user artifact root.
const CampaignStateDir = ".campaign"

// enclosingRoot returns the declared root that already covers rel, if any.
func enclosingRoot(cfg *File, rel string) (string, bool) {
	if cfg == nil {
		return "", false
	}
	for _, r := range cfg.Roots {
		root := NormalizeRootPath(r.Path)
		if root == "" {
			continue
		}
		if rel == root || strings.HasPrefix(rel, root+"/") {
			return root, true
		}
	}
	return "", false
}

// isRepoRoot reports whether a campaign-relative directory is a git repository
// root. A submodule carries a .git file rather than a directory, so both
// spellings count.
func isRepoRoot(campRoot, rel string) bool {
	gitPath := filepath.Join(campRoot, filepath.FromSlash(rel), ".git")
	_, err := os.Lstat(gitPath)
	return err == nil
}

// SiblingRootCount counts declared roots that share a parent directory, so the
// CLI can suggest collapsing them. Camp suggests; it never collapses, because
// merging roots changes what syncs and only the user knows if that is wanted.
func SiblingRootCount(cfg *File, parent string) int {
	if cfg == nil || parent == "" {
		return 0
	}
	parent = NormalizeRootPath(parent)
	n := 0
	for _, r := range cfg.Roots {
		if parentDir(NormalizeRootPath(r.Path)) == parent {
			n++
		}
	}
	return n
}
