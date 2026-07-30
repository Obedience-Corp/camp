package checks

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Obedience-Corp/camp/internal/artifacts"
	"github.com/Obedience-Corp/camp/internal/doctor"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/fsutil"
	"github.com/Obedience-Corp/camp/internal/git"
)

// ArtifactsCheck validates every declared artifact root.
//
// A campaign can reach a bad artifact state without camp ever choosing it:
// artifacts.yaml is committed and hand-editable, roots are declared on one
// machine and evaluated on another, and the auto-declare path deliberately
// refuses cases a human can still write by hand. None of that is visible at
// commit time, because the commit path only sees what git status reports.
// This check is where an already-broken campaign becomes findable.
type ArtifactsCheck struct{}

// NewArtifactsCheck creates a new artifact root health check.
func NewArtifactsCheck() *ArtifactsCheck {
	return &ArtifactsCheck{}
}

// ID returns the check identifier.
func (c *ArtifactsCheck) ID() string {
	return "artifacts"
}

// Name returns the human-readable check name.
func (c *ArtifactsCheck) Name() string {
	return "Artifact Roots"
}

// Description returns a brief explanation of what this check does.
func (c *ArtifactsCheck) Description() string {
	return "Validates declared artifact roots against gitignore, location, and overlap rules"
}

// Detail keys carried on each issue so --json consumers can act on the row
// without parsing the description.
const (
	artifactsDetailRoot   = "root"
	artifactsDetailReason = "reason"
)

// Row reasons, one per condition in the design's table.
const (
	reasonCleanNotIgnored  = "clean_root_not_gitignored"
	reasonRootMissing      = "root_missing_locally"
	reasonEscapesCampaign  = "root_escapes_campaign"
	reasonNestedRoot       = "root_nested_in_declared_root"
	reasonCrossesSubmodule = "root_crosses_submodule_boundary"
	reasonDVCOverlap       = "root_also_dvc_tracked"
	reasonManifestDrift    = "manifest_drift"
)

// Run validates every declared root.
func (c *ArtifactsCheck) Run(ctx context.Context, repoRoot string) (*doctor.CheckResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	result := &doctor.CheckResult{
		Passed:  true,
		Total:   0,
		Issues:  make([]doctor.Issue, 0),
		Details: make(map[string]any),
	}

	cfg, err := artifacts.Load(repoRoot)
	if err != nil {
		return nil, camperrors.Wrap(err, "load artifact declarations")
	}
	result.Total = len(cfg.Roots)
	if len(cfg.Roots) == 0 {
		return result, nil
	}

	submodules, err := git.ListSubmodulePaths(ctx, repoRoot)
	if err != nil {
		// A campaign without submodules, or a repo git cannot read, should not
		// fail the whole check; the other rows are still worth reporting.
		submodules = nil
	}

	for _, root := range cfg.Roots {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		issues := c.inspectRoot(ctx, repoRoot, cfg, root, submodules)
		result.Issues = append(result.Issues, issues...)
	}

	// Criterion 27: manifest drift. Stat-level only, deliberately: the row
	// compares size, nanosecond mtime, and presence against this machine's
	// committed record. It reports and never resolves; the manifest is a
	// record of what was, and only a commit updates it.
	if machine, mErr := artifacts.MachineName(); mErr == nil {
		for _, root := range cfg.Roots {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			committed, describes, lErr := artifacts.LoadCommitted(repoRoot, machine, root.Path)
			if lErr != nil || committed == nil {
				continue // no committed record yet; nothing to drift from
			}
			drifts, dErr := artifacts.DetectDrift(ctx, repoRoot, committed)
			if dErr != nil || len(drifts) == 0 {
				continue
			}
			result.Issues = append(result.Issues, artifactIssue(
				c.ID(), root.Path, reasonManifestDrift, doctor.SeverityWarning,
				fmt.Sprintf("%s has drifted from its committed manifest (%d paths; record describes %s)",
					root.Path, len(drifts), shortCommit(describes)),
				"camp commit",
			))
		}
	}

	for _, issue := range result.Issues {
		if issue.Severity == doctor.SeverityError {
			result.Passed = false
			break
		}
	}
	return result, nil
}

// inspectRoot runs the condition table against one declared root.
func (c *ArtifactsCheck) inspectRoot(
	ctx context.Context,
	repoRoot string,
	cfg *artifacts.File,
	root artifacts.Root,
	submodules []string,
) []doctor.Issue {
	var issues []doctor.Issue
	normalized := artifacts.NormalizeRootPath(root.Path)

	// Location first. A root that escapes the campaign or sits inside another
	// repo cannot be meaningfully evaluated for the remaining rows, and its
	// remedy is different in kind: the declaration itself is wrong.
	if _, err := artifacts.EnsureRootWithin(repoRoot, root.Path); err != nil {
		return append(issues, artifactIssue(c.ID(), normalized, reasonEscapesCampaign,
			doctor.SeverityError,
			fmt.Sprintf("%s escapes the campaign root: %v", normalized, err),
			"camp artifacts remove "+normalized))
	}

	if parent, ok := enclosingDeclaredRoot(cfg, normalized); ok {
		issues = append(issues, artifactIssue(c.ID(), normalized, reasonNestedRoot,
			doctor.SeverityError,
			fmt.Sprintf("%s is nested inside declared root %s; one root should own it", normalized, parent),
			"camp artifacts remove "+normalized))
	}

	if sub, ok := crossesSubmodule(normalized, submodules); ok {
		issues = append(issues, artifactIssue(c.ID(), normalized, reasonCrossesSubmodule,
			doctor.SeverityError,
			fmt.Sprintf("%s crosses into submodule %s; artifact roots are campaign-relative, so its bytes "+
				"would never reach the submodule's remote. Camp never creates this state, so it was "+
				"hand-written into artifacts.yaml", normalized, sub),
			"camp artifacts remove "+normalized))
	}

	if dvcPath, ok := dvcGoverned(repoRoot, normalized); ok {
		issues = append(issues, artifactIssue(c.ID(), normalized, reasonDVCOverlap,
			doctor.SeverityError,
			fmt.Sprintf("%s is also tracked by DVC (%s); two systems owning the same bytes will fight",
				normalized, dvcPath),
			"camp artifacts remove "+normalized))
	}

	abs := filepath.Join(repoRoot, filepath.FromSlash(normalized))
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		// Not an error: a declared root legitimately lives on another machine
		// until synced, which is the whole point of declaring it centrally.
		return append(issues, artifactIssue(c.ID(), normalized, reasonRootMissing,
			doctor.SeverityWarning,
			fmt.Sprintf("%s is declared but not present locally; it may live on another machine", normalized),
			""))
	}

	// A mixed root is a supported state under Proposal A (tracked files are
	// git's, everything else is the artifact set). Reporting it as a finding
	// would train users to ignore this check, so it is never one.
	if artifacts.HasTrackedFiles(ctx, repoRoot, normalized) {
		return issues
	}
	if !artifacts.IsGitignored(ctx, repoRoot, normalized) {
		issues = append(issues, artifactIssue(c.ID(), normalized, reasonCleanNotIgnored,
			doctor.SeverityError,
			fmt.Sprintf("%s holds no tracked files but is not gitignored; its content will be committed", normalized),
			"camp doctor -c artifacts --fix"))
		issues[len(issues)-1].AutoFixable = true
	}
	return issues
}

// artifactIssue builds one finding with its machine-readable details.
func artifactIssue(checkID, root, reason string, severity doctor.Severity, desc, fixCmd string) doctor.Issue {
	return doctor.Issue{
		Severity:    severity,
		CheckID:     checkID,
		Description: desc,
		FixCommand:  fixCmd,
		Details: map[string]any{
			artifactsDetailRoot:   root,
			artifactsDetailReason: reason,
		},
	}
}

// Fix appends the ignore rule for clean roots that lack one.
//
// That is the only automatic repair here, and deliberately so. Every other row
// needs a decision camp cannot make: which root to keep when two nest, whether
// DVC or camp should own the bytes, whether a missing root should be synced or
// undeclared. Fix never untracks a file, because untracking is destructive to
// git history in a way appending a line is not.
func (c *ArtifactsCheck) Fix(ctx context.Context, repoPath string, issues []doctor.Issue) ([]doctor.Issue, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	fixed := make([]doctor.Issue, 0, len(issues))
	for _, issue := range issues {
		if issue.CheckID != c.ID() || !issue.AutoFixable {
			continue
		}
		if reason, _ := issue.Details[artifactsDetailReason].(string); reason != reasonCleanNotIgnored {
			continue
		}
		root, _ := issue.Details[artifactsDetailRoot].(string)
		if root == "" {
			continue
		}

		// Re-check rather than trusting the finding: state can change between
		// Run and Fix, and appending an ignore rule to a root that has since
		// gained tracked files would hide them from git.
		if artifacts.HasTrackedFiles(ctx, repoPath, root) {
			continue
		}
		wrote, err := fsutil.AppendGitignoreEntryIfMissing(
			filepath.Join(repoPath, ".gitignore"), root+"/", artifactsGitignoreComment)
		if err != nil {
			return fixed, camperrors.Wrapf(err, "add %q to .gitignore", root+"/")
		}
		if wrote {
			fixed = append(fixed, issue)
		}
	}
	return fixed, nil
}

// artifactsGitignoreComment explains the rule in the file itself.
const artifactsGitignoreComment = "# Camp artifact root (synced with 'camp sync', not git)"

// enclosingDeclaredRoot returns another declared root that already contains
// this one, if any.
func enclosingDeclaredRoot(cfg *artifacts.File, normalized string) (string, bool) {
	for _, other := range cfg.Roots {
		candidate := artifacts.NormalizeRootPath(other.Path)
		if candidate == "" || candidate == normalized {
			continue
		}
		if strings.HasPrefix(normalized, candidate+"/") {
			return candidate, true
		}
	}
	return "", false
}

// crossesSubmodule reports whether a root is at or inside a submodule.
func crossesSubmodule(normalized string, submodules []string) (string, bool) {
	for _, sub := range submodules {
		s := artifacts.NormalizeRootPath(sub)
		if s == "" {
			continue
		}
		if normalized == s || strings.HasPrefix(normalized, s+"/") {
			return s, true
		}
	}
	return "", false
}

// dvcGoverned reports whether DVC also tracks this root, via a sibling .dvc
// pointer file or a .dvc/ directory at or above it.
//
// Overlap matters because both systems move the same bytes out of git and
// then disagree about who restores them; the user has to pick one.
func dvcGoverned(repoRoot, normalized string) (string, bool) {
	pointer := filepath.Join(repoRoot, filepath.FromSlash(normalized)+".dvc")
	if _, err := os.Lstat(pointer); err == nil {
		return normalized + ".dvc", true
	}

	dir := normalized
	for {
		candidate := filepath.Join(repoRoot, filepath.FromSlash(dir), ".dvc")
		if info, err := os.Lstat(candidate); err == nil && info.IsDir() {
			return strings.TrimPrefix(dir+"/.dvc", "/"), true
		}
		parent := filepath.Dir(dir)
		if parent == dir || parent == "." {
			break
		}
		dir = parent
	}
	return "", false
}

// shortCommit abbreviates a SHA for a doctor row a human reads.
func shortCommit(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
