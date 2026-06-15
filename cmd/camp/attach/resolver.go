package attach

import (
	"context"
	"io"

	"github.com/Obedience-Corp/camp/cmd/camp/cmdutil"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	navtui "github.com/Obedience-Corp/camp/internal/nav/tui"
)

// attachResolver picks a target campaign for camp attach. It mirrors the
// linked-project resolver but skips project-specific registry checks because
// attachments are not registered as projects.
type attachResolver struct {
	stderr        io.Writer
	usageLine     string
	isInteractive func() bool
	loadCurrent   func(context.Context) (*config.CampaignConfig, string, error)
	loadRegistry  func(context.Context) (*config.Registry, error)
	loadCampaign  func(context.Context, string) (*config.CampaignConfig, error)
	updateAccess  func(context.Context, string) error
	pickCampaign  func(context.Context, *config.Registry) (config.RegisteredCampaign, error)
}

// NewResolver returns a CampaignResolver suitable for camp attach.
func NewResolver(stderr io.Writer, usageLine string) CampaignResolver {
	return attachResolver{
		stderr:        stderr,
		usageLine:     usageLine,
		isInteractive: navtui.IsTerminal,
		loadCurrent:   config.LoadCampaignConfigFromCwd,
		loadRegistry:  config.LoadRegistry,
		loadCampaign:  config.LoadCampaignConfig,
		updateAccess:  updateAttachRegistryLastAccess,
		pickCampaign:  cmdutil.PickCampaign,
	}
}

func (r attachResolver) Resolve(ctx context.Context, targetCampaign string, targetChanged bool) (*config.CampaignConfig, string, error) {
	if targetCampaign == NoOptCampaign {
		targetCampaign = ""
	}

	if !targetChanged {
		cfg, root, err := r.loadCurrent(ctx)
		if err == nil {
			return cfg, root, nil
		}
	}

	reg, err := r.loadRegistry(ctx)
	if err != nil {
		return nil, "", camperrors.Wrap(err, "load registry")
	}
	if reg.Len() == 0 {
		return nil, "", camperrors.Wrap(camperrors.ErrNotInitialized,
			"no campaigns registered (use 'camp init' to create one)")
	}

	var selected config.RegisteredCampaign
	switch targetCampaign {
	case "":
		if !r.isInteractive() {
			return nil, "", camperrors.Wrapf(camperrors.ErrInvalidInput,
				"campaign name required in non-interactive mode\n       Usage: %s", r.usageLine)
		}
		selected, err = r.pickCampaign(ctx, reg)
		if err != nil {
			return nil, "", err
		}
	default:
		selected, err = cmdutil.ResolveCampaignSelection(targetCampaign, reg, r.stderr)
		if err != nil {
			return nil, "", err
		}
	}

	cfg, err := r.loadCampaign(ctx, selected.Path)
	if err != nil {
		return nil, "", camperrors.Wrapf(err, "load target campaign %s", selected.Path)
	}

	if r.updateAccess != nil {
		_ = r.updateAccess(ctx, selected.ID)
	}

	return cfg, selected.Path, nil
}

func updateAttachRegistryLastAccess(ctx context.Context, id string) error {
	return config.UpdateRegistry(ctx, func(reg *config.Registry) error {
		reg.UpdateLastAccess(id)
		return nil
	})
}
