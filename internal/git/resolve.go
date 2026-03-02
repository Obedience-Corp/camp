package git

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// TargetResult holds the resolved repository path and metadata.
type TargetResult struct {
	// Path is the resolved absolute path to the target repository.
	Path string
	// IsSubmodule indicates whether the target is a submodule.
	IsSubmodule bool
	// Name is a display name (submodule directory name or "campaign root").
	Name string
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
		Name:        "campaign root",
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

	return &TargetResult{
		Path:        root,
		IsSubmodule: isSubmodule,
		Name:        filepath.Base(root),
	}, nil
}

// resolveFromCwd auto-detects the submodule from the current working directory.
func resolveFromCwd(_ context.Context, campaignRoot string) (*TargetResult, error) {
	root, isSubmodule, err := FindProjectRootWithType(".")
	if err != nil {
		return nil, camperrors.Wrap(err, "cannot detect git repository from current directory")
	}

	// If we're in the campaign root, just return it
	absRoot, _ := filepath.Abs(root)
	absCamp, _ := filepath.Abs(campaignRoot)
	if absRoot == absCamp {
		return &TargetResult{
			Path:        campaignRoot,
			IsSubmodule: false,
			Name:        "campaign root",
		}, nil
	}

	if !isSubmodule {
		return nil, fmt.Errorf("current directory is in a git repository but not a submodule of the campaign")
	}

	return &TargetResult{
		Path:        root,
		IsSubmodule: true,
		Name:        filepath.Base(root),
	}, nil
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

// ExtractSubFlags extracts --sub and --project/-p flags from a raw args slice.
// Returns the remaining args (to pass to git) and the flag values.
// This is used by commands with DisableFlagParsing that need to extract
// camp-specific flags before passing the rest to git.
func ExtractSubFlags(args []string) (remaining []string, sub bool, project string) {
	remaining = make([]string, 0, len(args))

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case arg == "--sub":
			sub = true

		case arg == "--project" || arg == "-p":
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

	return remaining, sub, project
}
