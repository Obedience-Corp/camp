package git

import (
	"context"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// TargetResult holds the resolved repository path and metadata.
type TargetResult struct {
	// Path is the resolved absolute path to the target repository.
	Path string
	// IsSubmodule indicates whether the target is a git submodule checkout.
	IsSubmodule bool
	// IsWorktree indicates whether the target is a linked worktree of a
	// campaign project (including a worktree of a submodule).
	IsWorktree bool
	// Name is a display name (project name, worktree owner, or "camp root").
	Name string
}

// IsNestedRepo reports whether the target is a nested campaign checkout
// (submodule or linked project worktree) rather than the campaign root.
func (t *TargetResult) IsNestedRepo() bool {
	return t != nil && (t.IsSubmodule || t.IsWorktree)
}

// NestedKindTitle returns "Worktree" or "Submodule" for nested checkouts.
func (t *TargetResult) NestedKindTitle() string {
	if t == nil {
		return ""
	}
	switch {
	case t.IsWorktree:
		return "Worktree"
	case t.IsSubmodule:
		return "Submodule"
	default:
		return ""
	}
}

// ResolveTarget determines the git repository path based on submodule flags.
// If sub is true, auto-detects the submodule from the current directory.
// If project is non-empty, resolves it as a campaign-relative or absolute path.
// If neither, returns the campaign root.
func ResolveTarget(ctx context.Context, campaignRoot string, sub bool, project string) (*TargetResult, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Explicit project path takes precedence
	if project != "" {
		return resolveProjectPath(ctx, campaignRoot, project)
	}

	// Auto-detect submodule from cwd
	if sub {
		return resolveFromCwd(ctx, campaignRoot)
	}

	// Default: campaign root
	return &TargetResult{
		Path:        campaignRoot,
		IsSubmodule: false,
		Name:        "camp root",
	}, nil
}

// resolveProjectPath resolves an explicit project path relative to campaign root.
func resolveProjectPath(_ context.Context, campaignRoot, project string) (*TargetResult, error) {
	var targetPath string

	if filepath.IsAbs(project) {
		targetPath = project
	} else {
		targetPath = filepath.Join(campaignRoot, project)
	}

	// Verify it's a git repository
	root, isSubmodule, err := FindProjectRootWithType(targetPath)
	if err != nil {
		return nil, camperrors.Wrapf(err, "project path %q is not a git repository", project)
	}

	return targetFromRoot(campaignRoot, root, isSubmodule), nil
}

// resolveFromCwd auto-detects the submodule from the current working directory.
func resolveFromCwd(_ context.Context, campaignRoot string) (*TargetResult, error) {
	root, isSubmodule, err := FindProjectRootWithType(".")
	if err != nil {
		return nil, camperrors.Wrap(err, "cannot detect git repository from current directory")
	}

	// If we're in the campaign root, just return it
	if sameFilesystemPath(root, campaignRoot) {
		return &TargetResult{
			Path:        campaignRoot,
			IsSubmodule: false,
			Name:        "camp root",
		}, nil
	}

	result := targetFromRoot(campaignRoot, root, isSubmodule)
	if result.IsNestedRepo() {
		return result, nil
	}

	return nil, camperrors.Newf("current directory is in a git repository but not a submodule of the camp")
}

func targetFromRoot(campaignRoot, root string, isSubmodule bool) *TargetResult {
	result := &TargetResult{
		Path:        root,
		IsSubmodule: isSubmodule,
		Name:        filepath.Base(root),
	}
	if isSubmodule {
		return result
	}
	if name, ok := nestedWorktreeOwner(campaignRoot, root); ok {
		result.IsWorktree = true
		result.Name = name
	}
	return result
}

// nestedWorktreeOwner returns the owning project name for a linked worktree of
// a campaign project. Prefers the projects/worktrees/<project>/<name> layout,
// then the gitdir under .git/modules/<path>/worktrees/<name>.
func nestedWorktreeOwner(campaignRoot, repoRoot string) (string, bool) {
	gitDir, err := ResolveGitDir(repoRoot)
	if err != nil || !isWorktreeGitDir(gitDir) {
		return "", false
	}

	if name, ok := projectNameFromWorktreesLayout(campaignRoot, repoRoot); ok {
		return name, true
	}
	if name, ok := projectNameFromModulesWorktree(filepath.Join(campaignRoot, ".git", "modules"), gitDir); ok {
		return name, true
	}
	if resolvedCamp, err := filepath.EvalSymlinks(campaignRoot); err == nil && resolvedCamp != campaignRoot {
		if name, ok := projectNameFromWorktreesLayout(resolvedCamp, repoRoot); ok {
			return name, true
		}
		if name, ok := projectNameFromModulesWorktree(filepath.Join(resolvedCamp, ".git", "modules"), gitDir); ok {
			return name, true
		}
	}
	return "", false
}

func projectNameFromWorktreesLayout(campaignRoot, repoRoot string) (string, bool) {
	return firstTwoRelSegments(filepath.Join(campaignRoot, "projects", "worktrees"), repoRoot)
}

func projectNameFromModulesWorktree(modulesRoot, gitDir string) (string, bool) {
	rel, ok := relWithin(modulesRoot, gitDir)
	if !ok {
		return "", false
	}
	const marker = "/worktrees/"
	idx := strings.LastIndex(rel, marker)
	if idx <= 0 {
		return "", false
	}
	modulePath := rel[:idx]
	base := filepath.Base(modulePath)
	if base == "." || base == ".." || base == "" {
		return "", false
	}
	return base, true
}

func firstTwoRelSegments(parent, child string) (string, bool) {
	rel, ok := relWithin(parent, child)
	if !ok {
		return "", false
	}
	parts := strings.Split(rel, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	return parts[0], true
}

func relWithin(parent, child string) (string, bool) {
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}

// HasPullStrategyFlag reports whether gitArgs contains a pull reconciliation
// strategy flag (--rebase, --no-rebase, --ff-only, --ff, --no-ff).
func HasPullStrategyFlag(gitArgs []string) bool {
	for _, arg := range gitArgs {
		switch arg {
		case "--rebase", "--no-rebase", "--ff-only", "--ff", "--no-ff":
			return true
		}
	}
	return false
}

// ExtractSubFlags extracts --sub, --project, and --no-drain from a raw args
// slice. Returns the remaining args (to pass to git) and the flag values.
// This is used by commands with DisableFlagParsing that need to extract
// camp-specific flags before passing the rest to git.
//
// --no-drain belongs here rather than on each command because it is peeled at
// exactly the same moment and for the same reason as the others: git has no
// such flag, so leaving it in the passthrough args would make git reject the
// command with a message about a flag camp invented.
func ExtractSubFlags(args []string) (remaining []string, sub bool, project string, noDrain bool) {
	remaining = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--":
			remaining = append(remaining, args[i:]...)
			return remaining, sub, project, noDrain

		case arg == "--sub":
			sub = true

		case arg == "--no-drain":
			noDrain = true

		case arg == "--project":
			// Next arg is the project path
			if i+1 < len(args) {
				i++
				project = args[i]
			}

		case strings.HasPrefix(arg, "--project="):
			project = strings.TrimPrefix(arg, "--project=")

		default:
			remaining = append(remaining, arg)
		}
	}

	return remaining, sub, project, noDrain
}

func sameFilesystemPath(a, b string) bool {
	canon := func(path string) string {
		abs, err := filepath.Abs(path)
		if err != nil {
			return filepath.Clean(path)
		}
		resolved, err := filepath.EvalSymlinks(abs)
		if err != nil {
			return filepath.Clean(abs)
		}
		return filepath.Clean(resolved)
	}
	return canon(a) == canon(b)
}
