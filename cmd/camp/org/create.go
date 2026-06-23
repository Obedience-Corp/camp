package org

import (
	"context"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

var orgCreateCmd = &cobra.Command{
	Use:   "create <org> [campaign...]",
	Short: "Create an org by joining campaigns (the current campaign if none named)",
	Long: `Create an org by joining campaigns to it.

Run inside a campaign with no campaign arguments to add the current campaign:
  camp org create obey

Or name the campaigns explicitly:
  camp org create obey obey-campaign obey-content

Orgs remain derived: "create" assigns membership and never makes an empty org.
Joining an org that already has members is allowed; there is no "already exists"
error, and a campaign already in the org is reported as unchanged.`,
	Example: `  camp org create obey
  camp org create client-acme acme-site other-site`,
	Args: cobra.MinimumNArgs(1),
	RunE: runOrgCreate,
}

func init() {
	Cmd.AddCommand(orgCreateCmd)
	orgCreateCmd.Flags().Bool("json", false, "Output as JSON")
}

func runOrgCreate(cmd *cobra.Command, args []string) error {
	org := args[0]
	if err := validateOrgName(org); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")

	campaignArgs := args[1:]
	if len(campaignArgs) == 0 {
		id, err := currentCampaignID(cmd.Context())
		if err != nil {
			return err
		}
		campaignArgs = []string{id}
	}
	return reassignOrg(cmd, func(*config.Registry) string { return org }, campaignArgs, asJSON)
}

func currentCampaignID(ctx context.Context) (string, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return "", camperrors.NewValidation("campaign",
			"not inside a campaign; name a campaign: camp org create <org> <campaign>", err)
	}
	cfg, err := config.LoadCampaignConfig(ctx, root)
	if err != nil {
		return "", err
	}
	reg, err := config.LoadRegistry(ctx)
	if err != nil {
		return "", camperrors.Wrap(err, "failed to load registry")
	}
	c, ok := reg.GetByID(cfg.ID)
	if !ok {
		return "", camperrors.NewValidation("campaign",
			"current campaign is not registered; name a campaign: camp org create <org> <campaign>", nil)
	}
	return c.ID, nil
}
