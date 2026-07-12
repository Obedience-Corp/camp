package org

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// defaultCreateCampaignInOrg calls the shared camp create/init registration path
// with --org, non-interactively. Used by the org TUI "N" action.
func defaultCreateCampaignInOrg(ctx context.Context, name, org string) error {
	if err := validateTUICampaignName(name); err != nil {
		return err
	}
	if err := config.ValidateName("org", org); err != nil {
		return err
	}
	cfg, err := config.LoadGlobalConfig(ctx)
	if err != nil {
		return camperrors.Wrap(err, "loading global config")
	}
	base, err := cfg.ResolvedCampaignsDir(ctx)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0o755); err != nil {
		return camperrors.Wrapf(err, "ensure campaigns dir %s", base)
	}
	target := filepath.Join(base, name)
	// Prefer empty-or-missing target; refuse non-empty without repair.
	if info, statErr := os.Stat(target); statErr == nil {
		if !info.IsDir() {
			return camperrors.New(fmt.Sprintf("target exists and is not a directory: %s", target))
		}
		entries, readErr := os.ReadDir(target)
		if readErr != nil {
			return camperrors.Wrap(readErr, "reading target")
		}
		if len(entries) > 0 {
			return camperrors.New(fmt.Sprintf("target %s exists and is not empty", target))
		}
	}
	p := initcmd.Params{
		Dir:         target,
		Name:        name,
		TypeStr:     string(config.CampaignTypeProduct),
		Description: "Created from camp org browser",
		Mission:     "Created from camp org browser",
		// Match `camp create` defaults (git init + skills linking) so a campaign
		// made from the org browser is not a silent subset of `camp create`.
		NoGit:    false,
		NoSkills: false,
		Org:      org,
	}
	// Non-interactive: description/mission already set, no forms.
	return initcmd.RunFlow(ctx, p, initcmd.Writers{HumanOut: os.Stderr}, false)
}

// validateTUICampaignName mirrors `camp create`'s single path-segment guard for
// the org TUI's new-campaign action. This function intentionally allows display
// names broader than config.ValidateName while rejecting path traversal.
func validateTUICampaignName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return camperrors.New("campaign name is empty")
	}
	if trimmed == "." || trimmed == ".." {
		return camperrors.New(fmt.Sprintf("invalid campaign name: %q", trimmed))
	}
	if strings.HasPrefix(trimmed, ".") {
		return camperrors.New(fmt.Sprintf("campaign name cannot start with '.': %q", trimmed))
	}
	if strings.ContainsAny(trimmed, "/\\") {
		return camperrors.New(fmt.Sprintf("campaign name cannot contain path separators: %q", trimmed))
	}
	return nil
}
