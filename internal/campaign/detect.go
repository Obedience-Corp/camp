package campaign

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// DefaultDetectTimeout is the maximum time to spend detecting a campaign root.
// This prevents hanging on slow network filesystems.
const DefaultDetectTimeout = 5 * time.Second

// CampaignDir is the name of the campaign marker directory.
const CampaignDir = ".campaign"

// EnvCampaignRoot is the environment variable that can override campaign detection.
const EnvCampaignRoot = "CAMP_ROOT"

// Detect finds the campaign root by walking up from startDir.
// Returns the directory containing .campaign/, not .campaign/ itself.
// If startDir is empty, uses the current working directory.
func Detect(ctx context.Context, startDir string) (string, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Preserve CAMP_ROOT's historical role as a hard override for out-of-tree
	// tooling and scripts. Linked-project detection adds more discovery paths,
	// but it should not weaken this existing contract.
	if envRoot, ok := detectCampaignRootOverride(); ok {
		return envRoot, nil
	}

	// Start from given directory or cwd
	dir := startDir
	if dir == "" {
		var err error
		dir, err = logicalWorkingDirectory()
		if err != nil {
			return "", err
		}
	}

	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Try the logical path first, then the resolved path if it differs.
	if root, err := detectCampaignByWalking(ctx, dir); err == nil {
		return root, nil
	} else if !errors.Is(err, ErrNotInCampaign) {
		return "", err
	}

	realDir, err := filepath.EvalSymlinks(dir)
	if err == nil && realDir != dir {
		if root, walkErr := detectCampaignByWalking(ctx, realDir); walkErr == nil {
			return root, nil
		} else if !errors.Is(walkErr, ErrNotInCampaign) {
			return "", walkErr
		}
	}

	return "", ErrNotInCampaign
}

// logicalWorkingDirectory recovers the shell's logical working directory when
// the process cwd has been entered through a symlink. os.Getwd may return the
// physical target path, which loses the campaign-local symlink context needed
// to resolve shared attachments. PWD is only trusted when it is absolute,
// exists, and resolves to the same physical directory as os.Getwd.
func logicalWorkingDirectory() (string, error) {
	physical, err := os.Getwd()
	if err != nil {
		return "", err
	}
	physical, err = filepath.Abs(physical)
	if err != nil {
		return "", err
	}
	physical = filepath.Clean(physical)

	logical := os.Getenv("PWD")
	if logical == "" || !filepath.IsAbs(logical) {
		return physical, nil
	}
	logical, err = filepath.Abs(logical)
	if err != nil {
		return physical, nil
	}
	logical = filepath.Clean(logical)
	if _, err := os.Stat(logical); err != nil {
		return physical, nil
	}

	resolvedPhysical, err := filepath.EvalSymlinks(physical)
	if err != nil {
		return physical, nil
	}
	resolvedLogical, err := filepath.EvalSymlinks(logical)
	if err != nil {
		return physical, nil
	}
	if resolvedPhysical == resolvedLogical {
		return logical, nil
	}

	return physical, nil
}

func detectCampaignRootOverride() (string, bool) {
	envRoot := os.Getenv(EnvCampaignRoot)
	if envRoot == "" {
		return "", false
	}

	if absRoot, err := filepath.Abs(envRoot); err == nil {
		envRoot = absRoot
	}
	if resolvedRoot, err := filepath.EvalSymlinks(envRoot); err == nil {
		envRoot = resolvedRoot
	}
	if !IsCampaignRoot(envRoot) {
		return "", false
	}

	return envRoot, true
}

func detectCampaignByWalking(ctx context.Context, startDir string) (string, error) {
	dir := startDir

	for {
		// Check context on each iteration for long walks
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		campaignPath := filepath.Join(dir, CampaignDir)
		info, err := os.Stat(campaignPath)
		if err == nil && info.IsDir() {
			if resolved, resolveErr := filepath.EvalSymlinks(dir); resolveErr == nil {
				return resolved, nil
			}
			return dir, nil
		}

		markerPath := filepath.Join(dir, LinkMarkerFile)
		marker, markerErr := ReadMarkerFile(markerPath)
		if markerErr == nil {
			if root, ok, resolveErr := resolveMarkerCampaignRoot(ctx, marker, dir); resolveErr != nil {
				return "", resolveErr
			} else if ok {
				return root, nil
			}
		}

		// Handle permission errors gracefully - just keep walking up
		// (we may not have permission to read a parent directory but
		// might still find the campaign root higher up)
		if err != nil && !os.IsNotExist(err) && !os.IsPermission(err) {
			// Some other error - return it
			return "", err
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ErrNotInCampaign
		}
		dir = parent
	}
}

func resolveMarkerCampaignRoot(ctx context.Context, marker *LinkMarker, startDir string) (string, bool, error) {
	if marker == nil {
		return "", false, nil
	}

	// When a shared attachment is reached through a campaign-local symlink,
	// prefer the campaign whose root contains the logical path. This preserves
	// the context of each symlink while keeping direct access deterministic via
	// active_campaign_id below.
	var fallback string
	campaignIDs := []string{marker.EffectiveCampaignID()}
	if marker.Kind == KindAttachment {
		campaignIDs = marker.EffectiveCampaignIDs()
	}
	for _, campaignID := range campaignIDs {
		root, found, err := lookupRegisteredCampaignRoot(ctx, campaignID)
		if err != nil {
			return "", false, err
		}
		if !found || !IsCampaignRoot(root) {
			continue
		}
		if campaignID == marker.EffectiveCampaignID() {
			fallback = root
		}
		if pathWithin(startDir, root) {
			return root, true, nil
		}
	}
	if fallback != "" {
		return fallback, true, nil
	}

	// Legacy fallback for pre-v2 markers that persisted campaign roots.
	if marker.CampaignRoot != "" {
		root, err := filepath.Abs(marker.CampaignRoot)
		if err != nil {
			return "", false, err
		}
		if resolved, err := filepath.EvalSymlinks(root); err == nil {
			root = resolved
		}
		if IsCampaignRoot(root) {
			return root, true, nil
		}
	}

	return "", false, nil
}

// pathWithin reports whether path is inside root using the logical path. The
// logical path is intentional: callers may be inside a campaign-local
// symlink, and resolving it first would erase the context needed to select a
// shared attachment's campaign.
func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// DetectFromCwd is a convenience function that detects from current working directory.
func DetectFromCwd(ctx context.Context) (string, error) {
	return Detect(ctx, "")
}

// IsCampaignRoot checks if the given directory is a campaign root (contains .campaign/).
func IsCampaignRoot(dir string) bool {
	campaignPath := filepath.Join(dir, CampaignDir)
	info, err := os.Stat(campaignPath)
	return err == nil && info.IsDir()
}
