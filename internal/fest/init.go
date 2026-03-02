package fest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// InitOptions configures the fest init operation.
type InitOptions struct {
	CampaignRoot string
	Verbose      bool
}

// RunInit executes fest init in the campaign's festivals directory.
func RunInit(ctx context.Context, opts *InitOptions) error {
	festivalsDir := filepath.Join(opts.CampaignRoot, "festivals")

	// Check if already initialized first (before requiring fest)
	if isAlreadyInitialized(festivalsDir) {
		return nil // Already done
	}

	festPath, err := FindFestCLI()
	if err != nil {
		return err
	}

	// Run fest init with timeout
	// Note: fest init <path> creates a "festivals" subdirectory inside <path>
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, festPath, "init", opts.CampaignRoot)
	cmd.Dir = opts.CampaignRoot

	if opts.Verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	if err := cmd.Run(); err != nil {
		return camperrors.Wrapf(err, "fest init failed in %s", festivalsDir)
	}

	return nil
}

// isAlreadyInitialized checks if festivals directory has fest structure.
func isAlreadyInitialized(festivalsDir string) bool {
	// Check for .festival directory or fest.yaml
	indicators := []string{
		filepath.Join(festivalsDir, ".festival"),
		filepath.Join(festivalsDir, "fest.yaml"),
		filepath.Join(festivalsDir, ".fest"),
	}

	for _, path := range indicators {
		if _, err := os.Stat(path); err == nil {
			return true
		}
	}
	return false
}

// IsInitialized checks if festivals directory is initialized.
func IsInitialized(campaignRoot string) bool {
	return isAlreadyInitialized(filepath.Join(campaignRoot, "festivals"))
}
