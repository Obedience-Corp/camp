package org

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	initcmd "github.com/Obedience-Corp/camp/cmd/camp/init"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

// defaultCreateCampaignInOrg calls the shared camp create/init registration path
// with --org, non-interactively. Used by the org TUI "N" action.
func defaultCreateCampaignInOrg(ctx context.Context, name, org string) error {
	if name == "" {
		return camperrors.New("campaign name is empty")
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
		NoGit:       true,
		NoSkills:    true,
		Org:         org,
	}
	// Non-interactive: description/mission already set, no forms.
	return initcmd.RunFlow(ctx, p, initcmd.Writers{HumanOut: os.Stderr}, false)
}
