package config

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// ResolvedCampaignsDir returns the absolute path for the campaigns directory.
// Resolution rules (in order):
//  1. If CampaignsDir is empty or all-whitespace, use defaultCampaignsDirTilde.
//  2. If the (trimmed) value starts with "~", expand the tilde against $HOME.
//  3. If the result is still relative, join it with $HOME.
//  4. Return filepath.Clean of the final absolute path.
func (c *GlobalConfig) ResolvedCampaignsDir(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw := strings.TrimSpace(c.CampaignsDir)
	if raw == "" {
		raw = defaultCampaignsDirTilde
	}
	if strings.HasPrefix(raw, "~") {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", camperrors.Wrap(err, "resolving $HOME for campaigns_dir")
		}
		raw = filepath.Join(home, strings.TrimPrefix(raw, "~"))
	}
	if !filepath.IsAbs(raw) {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", camperrors.Wrap(err, "resolving $HOME for relative campaigns_dir")
		}
		raw = filepath.Join(home, raw)
	}
	return filepath.Clean(raw), nil
}
