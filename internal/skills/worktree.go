package skills

import (
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Obedience-Corp/camp/internal/campaign"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// WorktreeSkillDestRels are the tool skill directories projected into each
// campaign project worktree. Harnesses such as Grok stop skill discovery at
// the worktree git root; projecting here makes campaign skills visible without
// requiring the session CWD to be the campaign root.
var WorktreeSkillDestRels = []string{
	".agents/skills",
	".claude/skills",
}

// GrokSkillsRel is the Grok-preferred skills path inside a worktree. It is a
// symlink to .agents/skills (same pattern as campaign-root .grok/skills).
const GrokSkillsRel = ".grok/skills"

// WorktreeProjection is one worktree destination after projection.
type WorktreeProjection struct {
	// Path is the absolute worktree root.
	Path string
	// RelPath is Path relative to campaignRoot when provided; otherwise Path.
	RelPath string
	// Agents is the projection summary for .agents/skills (primary status).
	Agents ProjectionSummary
	// Claude is the projection summary for .claude/skills.
	Claude ProjectionSummary
	// Err is a hard failure projecting into this worktree (skipped otherwise).
	Err error
}

// ListWorktreeRoots returns absolute paths of project worktrees under
// worktreesRoot, which uses the campaign layout:
//
//	worktreesRoot/<project>/<name>/
//
// Only directories that look like git checkouts (have a .git file or
// directory) are returned, so ordinary project subdirs (src, bin,
// node_modules, …) under a mis-nested tree are ignored. Missing
// worktreesRoot yields an empty list, not an error.
func ListWorktreeRoots(worktreesRoot string) ([]string, error) {
	entries, err := os.ReadDir(worktreesRoot)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, camperrors.Wrap(err, "list worktrees root")
	}

	var roots []string
	for _, projectEntry := range entries {
		if !projectEntry.IsDir() || strings.HasPrefix(projectEntry.Name(), ".") {
			continue
		}
		projectDir := filepath.Join(worktreesRoot, projectEntry.Name())
		// Some checkouts land directly under worktreesRoot/<name> (no project
		// nesting). Accept those when they are git roots.
		if isGitCheckoutRoot(projectDir) {
			roots = append(roots, projectDir)
			continue
		}
		wtEntries, err := os.ReadDir(projectDir)
		if err != nil {
			return nil, camperrors.Wrapf(err, "list worktrees for project %s", projectEntry.Name())
		}
		for _, wtEntry := range wtEntries {
			if !wtEntry.IsDir() || strings.HasPrefix(wtEntry.Name(), ".") {
				continue
			}
			wtPath := filepath.Join(projectDir, wtEntry.Name())
			if !isGitCheckoutRoot(wtPath) {
				continue
			}
			roots = append(roots, wtPath)
		}
	}
	sort.Strings(roots)
	return roots, nil
}

// isGitCheckoutRoot reports whether path is a git working tree (main repo or
// linked worktree). Linked worktrees use a .git file; main repos use a .git
// directory.
func isGitCheckoutRoot(path string) bool {
	_, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil
}

// ProjectIntoWorktree projects skill bundles from skillsDir into a single
// worktree root (.agents/skills and .claude/skills), then ensures
// .grok/skills → .agents/skills so Grok discovers the same set.
//
// campaignRoot is used only for ValidateDestination; it must contain the
// worktree. An empty slugs list is a successful no-op.
func ProjectIntoWorktree(worktreeRoot, campaignRoot, skillsDir string, slugs []string, dryRun, force bool, errOut io.Writer) (WorktreeProjection, error) {
	out := WorktreeProjection{Path: worktreeRoot}
	if rel, err := filepath.Rel(campaignRoot, worktreeRoot); err == nil && !strings.HasPrefix(rel, "..") {
		out.RelPath = rel
	} else {
		out.RelPath = worktreeRoot
	}

	if len(slugs) == 0 {
		return out, nil
	}

	for _, rel := range WorktreeSkillDestRels {
		dest := filepath.Join(worktreeRoot, rel)
		if err := ValidateDestination(dest, campaignRoot); err != nil {
			out.Err = err
			return out, err
		}
		if err := EnsureProjectionDirectory(dest, dryRun, errOut); err != nil {
			out.Err = err
			return out, err
		}
		summary, err := ProjectSkillEntries(dest, skillsDir, slugs, dryRun, force)
		if err != nil {
			out.Err = err
			return out, err
		}
		switch rel {
		case ".agents/skills":
			out.Agents = summary
		case ".claude/skills":
			out.Claude = summary
		}
	}

	if err := EnsureGrokSkillsAlias(worktreeRoot, dryRun); err != nil {
		out.Err = err
		return out, err
	}
	return out, nil
}

// EnsureGrokSkillsAlias creates worktreeRoot/.grok/skills as a symlink to
// ../.agents/skills when missing or incorrect. It never overwrites a real
// directory or a non-managed symlink that points elsewhere without force
// semantics — here a foreign real directory is left alone and reported as an error
// only when it blocks a clean alias; a correct existing link is a no-op.
func EnsureGrokSkillsAlias(worktreeRoot string, dryRun bool) error {
	linkPath := filepath.Join(worktreeRoot, GrokSkillsRel)
	// Target relative to the symlink's parent (.grok/): ../.agents/skills
	const relTarget = "../.agents/skills"
	wantAbs := filepath.Clean(filepath.Join(filepath.Dir(linkPath), relTarget))

	info, err := os.Lstat(linkPath)
	if err == nil {
		if info.Mode()&os.ModeSymlink == 0 {
			if info.IsDir() {
				// Real directory: leave it; Grok can still scan it if populated.
				return nil
			}
			return camperrors.Newf("refusing to replace non-symlink at %s", linkPath)
		}
		raw, readErr := os.Readlink(linkPath)
		if readErr != nil {
			return camperrors.Wrap(readErr, "read grok skills alias")
		}
		gotAbs := resolveSymlinkTargetAbs(linkPath, raw)
		if filepath.Clean(gotAbs) == wantAbs {
			return nil
		}
		// Wrong target: replace when not dry-run.
		if dryRun {
			return nil
		}
		if err := os.Remove(linkPath); err != nil {
			return camperrors.Wrap(err, "remove stale grok skills alias")
		}
	} else if !os.IsNotExist(err) {
		return camperrors.Wrap(err, "stat grok skills alias")
	}

	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return camperrors.Wrap(err, "create .grok directory")
	}
	if err := os.Symlink(relTarget, linkPath); err != nil {
		return camperrors.Wrap(err, "create grok skills alias")
	}
	return nil
}

// LinkAllWorktrees projects skill bundles into every worktree under
// worktreesRoot. Per-worktree errors are recorded on WorktreeProjection.Err;
// a top-level error is returned only when the worktree list or skill discovery
// cannot be read.
func LinkAllWorktrees(campaignRoot, worktreesRoot, skillsDir string, dryRun, force bool, errOut io.Writer) ([]WorktreeProjection, error) {
	slugs, err := DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return nil, camperrors.Wrap(err, "discover skill bundles")
	}
	roots, err := ListWorktreeRoots(worktreesRoot)
	if err != nil {
		return nil, err
	}
	results := make([]WorktreeProjection, 0, len(roots))
	for _, root := range roots {
		proj, projErr := ProjectIntoWorktree(root, campaignRoot, skillsDir, slugs, dryRun, force, errOut)
		if projErr != nil && proj.Err == nil {
			proj.Err = projErr
		}
		results = append(results, proj)
	}
	return results, nil
}

// ProjectIntoWorktreeBestEffort projects campaign skills into a newly created
// worktree. Missing .campaign/skills or an empty skills dir is a silent
// success (no-op). Other errors are returned so callers can warn without
// failing worktree creation.
func ProjectIntoWorktreeBestEffort(campaignRoot, worktreePath string) error {
	skillsDir := filepath.Join(campaignRoot, campaign.CampaignDir, SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return camperrors.Wrap(err, "stat campaign skills")
	}
	slugs, err := DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return err
	}
	if len(slugs) == 0 {
		return nil
	}
	_, err = ProjectIntoWorktree(worktreePath, campaignRoot, skillsDir, slugs, false, false, io.Discard)
	return err
}

// InspectWorktreeProjection reports projection state for a worktree's
// .agents/skills directory (primary harness path).
func InspectWorktreeProjection(worktreeRoot, skillsDir string, slugs []string) (ProjectionState, error) {
	dest := filepath.Join(worktreeRoot, ".agents/skills")
	pathType, err := CheckPathType(dest)
	if err != nil {
		return ProjectionState{}, err
	}
	switch pathType {
	case TypeMissing:
		return ProjectionState{TotalSkills: len(slugs)}, nil
	case TypeFile, TypeSymlink:
		return ProjectionState{TotalSkills: len(slugs), Conflicts: 1}, nil
	case TypeDirectory:
		return InspectSkillProjection(dest, skillsDir, slugs)
	default:
		return ProjectionState{TotalSkills: len(slugs)}, nil
	}
}
