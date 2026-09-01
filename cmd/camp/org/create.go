package org

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

var orgCreateCmd = &cobra.Command{
	Use:   "create <org> [camp...]",
	Short: "Create an org (optionally empty) and join camps",
	Long: `Create a first-class org, optionally joining camps to it.

Run inside a camp with no camp arguments to add the current camp:
  camp org create obey

Or name the camps explicitly:
  camp org create obey obey-campaign obey-content

Create an empty org with no members (works outside a camp):
  camp org create obey --empty

Orgs are first-class: they persist in the registry even with zero members.
Joining an org that already has members is allowed; there is no "already exists"
error, and a camp already in the org is reported as unchanged.`,
	Example: `  camp org create obey
  camp org create obey --empty
  camp org create client-acme acme-site other-site`,
	Args: cobra.MinimumNArgs(1),
	RunE: runOrgCreate,
}

func init() {
	Cmd.AddCommand(orgCreateCmd)
	orgCreateCmd.Flags().Bool("json", false, "Output as JSON")
	orgCreateCmd.Flags().Bool("empty", false, "Create the org with no members (do not join any camp)")
}

func runOrgCreate(cmd *cobra.Command, args []string) error {
	org := args[0]
	if err := validateOrgName(org); err != nil {
		return err
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	empty, _ := cmd.Flags().GetBool("empty")
	if empty {
		if len(args) > 1 {
			return camperrors.NewValidation("empty", "--empty takes no camp arguments", nil)
		}
		return createEmptyOrg(cmd, org, asJSON)
	}

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

type orgCreateEmptyResult struct {
	Org     string `json:"org"`
	Created bool   `json:"created"`
	Members int    `json:"members"`
}

func createEmptyOrg(cmd *cobra.Command, org string, asJSON bool) error {
	created := false
	members := 0
	err := config.UpdateRegistry(cmd.Context(), func(reg *config.Registry) error {
		if !orgExists(reg, org) {
			created = true
		}
		ensureOrg(reg, org)
		members = len(membersOf(reg, org))
		return nil
	})
	if err != nil {
		return err
	}
	result := orgCreateEmptyResult{Org: org, Created: created, Members: members}
	if asJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}
	if created {
		_, err := fmt.Fprintf(cmd.OutOrStdout(), "created org %q (%d members)\n", org, members)
		return err
	}
	_, err = fmt.Fprintf(cmd.OutOrStdout(), "org %q already exists (%d members)\n", org, members)
	return err
}

func currentCampaignID(ctx context.Context) (string, error) {
	root, err := campaign.DetectCached(ctx)
	if err != nil {
		return "", camperrors.NewValidation("camp",
			"not inside a camp; name a camp: camp org create <org> <camp>", err)
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
		return "", camperrors.NewValidation("camp",
			"current camp is not registered; name a camp: camp org create <org> <camp>", nil)
	}
	return c.ID, nil
}
