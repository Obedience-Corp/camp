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

	// Start from given directory or cwd
	dir := startDir
	if dir == "" {
		var err error
		dir, err = os.Getwd()
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

	// Validate CAMP_ROOT as a compatibility fallback instead of a blind override.
	if envRoot := os.Getenv(EnvCampaignRoot); envRoot != "" {
		envRoot, err = filepath.Abs(envRoot)
		if err == nil && IsCampaignRoot(envRoot) {
			if isPathWithin(dir, envRoot) || (realDir != "" && isPathWithin(realDir, envRoot)) {
				return envRoot, nil
			}
		}
	}

	return "", ErrNotInCampaign
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
		if markerErr == nil && marker.CampaignRoot != "" {
			root, absErr := filepath.Abs(marker.CampaignRoot)
			if absErr == nil && IsCampaignRoot(root) {
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

func isPathWithin(child, parent string) bool {
	if child == "" || parent == "" {
		return false
	}
	if child == parent {
		return true
	}
	rel, err := filepath.Rel(parent, child)
	if err != nil {
		return false
	}
	return rel != ".." && rel != "." && rel != "" && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// DetectWithTimeout detects campaign root with a timeout.
// If the filesystem is slow (e.g., network drives), detection will
// be aborted after the default timeout to prevent hanging.
func DetectWithTimeout(startDir string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultDetectTimeout)
	defer cancel()
	return Detect(ctx, startDir)
}

// DetectFromCwdWithTimeout is a convenience function that detects from current
// working directory with a timeout.
func DetectFromCwdWithTimeout() (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultDetectTimeout)
	defer cancel()
	return Detect(ctx, "")
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

// CampaignPath returns the path to the .campaign/ directory for a given root.
func CampaignPath(root string) string {
	return filepath.Join(root, CampaignDir)
}
