package campaign

import (
	"context"
	"os"
	"path/filepath"
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
// If CAMP_ROOT environment variable is set, uses that instead of detection.
func Detect(ctx context.Context, startDir string) (string, error) {
	// Check context cancellation
	if ctx.Err() != nil {
		return "", ctx.Err()
	}

	// Check for environment variable override
	if envRoot := os.Getenv(EnvCampaignRoot); envRoot != "" {
		// Verify the env var points to a valid campaign
		campaignPath := filepath.Join(envRoot, CampaignDir)
		if info, err := os.Stat(campaignPath); err == nil && info.IsDir() {
			return envRoot, nil
		}
		// If env var is set but invalid, continue with detection
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

	// Resolve to absolute path and follow symlinks
	dir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	// Walk up directory tree
	for {
		// Check context on each iteration for long walks
		if ctx.Err() != nil {
			return "", ctx.Err()
		}

		campaignPath := filepath.Join(dir, CampaignDir)
		info, err := os.Stat(campaignPath)
		if err == nil && info.IsDir() {
			return dir, nil
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
			// Reached filesystem root
			return "", ErrNotInCampaign
		}
		dir = parent
	}
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
