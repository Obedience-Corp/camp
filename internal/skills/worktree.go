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
// worktreesRoot.
//
// Expected campaign layout (what camp project worktree add creates):
//
//	worktreesRoot/<project>/<name>/   # git worktree root
//
// Only directories that look like git checkouts (have a .git file or
// directory) are returned, so ordinary project subdirs (src, bin,
// node_modules, …) under a mis-nested tree are ignored.
//
// If worktreesRoot/<project> is itself a git root (a loose checkout without
// the <name> level), that directory is projected and its children are not
// scanned — nested worktrees under a git-root project dir are out of scope.
// Missing worktreesRoot yields an empty list, not an error.
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
		// Loose checkout: worktreesRoot/<name> is itself the git root (no
		// project nesting). Project it and do not descend — children are
		// package dirs, not nested worktrees under this layout contract.
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

	grokSummary, err := ensureWorktreeGrokSkills(worktreeRoot, skillsDir, slugs, dryRun, force, errOut)
	if err != nil {
		out.Err = err
		return out, err
	}
	// Surface grok conflicts on the agents summary so link/status aggregation
	// already counts them (same ConflictNames path as tool dirs).
	out.Agents.Conflicts += grokSummary.Conflicts
	out.Agents.ConflictNames = append(out.Agents.ConflictNames, grokSummary.ConflictNames...)
	out.Agents.Created += grokSummary.Created
	out.Agents.Replaced += grokSummary.Replaced
	out.Agents.AlreadyLinked += grokSummary.AlreadyLinked
	return out, nil
}

// grokAliasRelTarget is the symlink target for .grok/skills relative to .grok/.
const grokAliasRelTarget = "../.agents/skills"

// ensureWorktreeGrokSkills makes Grok discover the same campaign skills as
// .agents/skills:
//
//   - missing path → symlink .grok/skills → ../.agents/skills
//   - correct symlink → no-op
//   - wrong symlink → conflict unless force (then replace), matching ProjectSkillEntries
//   - real directory → project per-bundle managed links into it (do not replace the dir)
//   - plain file → conflict
func ensureWorktreeGrokSkills(worktreeRoot, skillsDir string, slugs []string, dryRun, force bool, errOut io.Writer) (ProjectionSummary, error) {
	linkPath := filepath.Join(worktreeRoot, GrokSkillsRel)
	wantAbs := filepath.Clean(filepath.Join(filepath.Dir(linkPath), grokAliasRelTarget))

	pathType, err := CheckPathType(linkPath)
	if err != nil {
		return ProjectionSummary{}, err
	}

	switch pathType {
	case TypeDirectory:
		// Real directory: Grok will scan it, so project managed bundles into it.
		return ProjectSkillEntries(linkPath, skillsDir, slugs, dryRun, force)

	case TypeFile:
		return ProjectionSummary{
			Conflicts:     1,
			ConflictNames: []string{GrokSkillsRel},
		}, nil

	case TypeSymlink:
		raw, readErr := os.Readlink(linkPath)
		if readErr != nil {
			return ProjectionSummary{}, camperrors.Wrap(readErr, "read grok skills alias")
		}
		gotAbs := resolveSymlinkTargetAbs(linkPath, raw)
		if filepath.Clean(gotAbs) == wantAbs {
			return ProjectionSummary{AlreadyLinked: 1}, nil
		}
		// Foreign / wrong-target symlink: require force, same as ProjectSkillEntries.
		if !force {
			return ProjectionSummary{
				Conflicts:     1,
				ConflictNames: []string{GrokSkillsRel},
			}, nil
		}
		if dryRun {
			return ProjectionSummary{Replaced: 1}, nil
		}
		if err := os.Remove(linkPath); err != nil {
			return ProjectionSummary{}, camperrors.Wrap(err, "remove foreign grok skills alias")
		}
		if err := createGrokSkillsAlias(worktreeRoot, dryRun); err != nil {
			return ProjectionSummary{}, err
		}
		return ProjectionSummary{Replaced: 1}, nil

	case TypeMissing:
		if err := createGrokSkillsAlias(worktreeRoot, dryRun); err != nil {
			return ProjectionSummary{}, err
		}
		return ProjectionSummary{Created: 1}, nil

	default:
		return ProjectionSummary{}, camperrors.Newf("unsupported path type for %s", linkPath)
	}
}

// createGrokSkillsAlias writes worktreeRoot/.grok/skills → ../.agents/skills.
func createGrokSkillsAlias(worktreeRoot string, dryRun bool) error {
	linkPath := filepath.Join(worktreeRoot, GrokSkillsRel)
	if dryRun {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
		return camperrors.Wrap(err, "create .grok directory")
	}
	if err := os.Symlink(grokAliasRelTarget, linkPath); err != nil {
		return camperrors.Wrap(err, "create grok skills alias")
	}
	return nil
}

// EnsureGrokSkillsAlias creates worktreeRoot/.grok/skills as a symlink to
// ../.agents/skills when missing. Prefer ensureWorktreeGrokSkills from
// ProjectIntoWorktree (handles force, directory projection, and conflicts).
// Kept for tests and callers that only need the default alias path with force.
func EnsureGrokSkillsAlias(worktreeRoot string, dryRun bool) error {
	_, err := ensureWorktreeGrokSkills(worktreeRoot, "", nil, dryRun, true, io.Discard)
	return err
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
// worktree. projected is true when at least one skill bundle was created or
// already linked under the worktree's .agents/skills (so callers can avoid a
// false "projected" success message). Missing .campaign/skills or an empty
// skills dir is a silent no-op (projected=false, err=nil). Other errors are
// returned so callers can warn without failing worktree creation.
func ProjectIntoWorktreeBestEffort(campaignRoot, worktreePath string) (projected bool, err error) {
	skillsDir := filepath.Join(campaignRoot, campaign.CampaignDir, SkillsSubdir)
	if _, err := os.Stat(skillsDir); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, camperrors.Wrap(err, "stat campaign skills")
	}
	slugs, err := DiscoverSkillSlugs(skillsDir)
	if err != nil {
		return false, err
	}
	if len(slugs) == 0 {
		return false, nil
	}
	proj, err := ProjectIntoWorktree(worktreePath, campaignRoot, skillsDir, slugs, false, false, io.Discard)
	if err != nil {
		return false, err
	}
	n := proj.Agents.Created + proj.Agents.Replaced + proj.Agents.AlreadyLinked
	return n > 0, nil
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
