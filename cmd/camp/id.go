package main

import (
	"fmt"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/spf13/cobra"
)

var idCmd = &cobra.Command{
	Use:          "id",
	Short:        "Print the current campaign ID",
	Long:         "Print the current campaign ID from .campaign/campaign.yaml.",
	Example:      "  camp id",
	Args:         cobra.NoArgs,
	SilenceUsage: true,
	RunE:         runID,
}

func init() {
	rootCmd.AddCommand(idCmd)
	idCmd.GroupID = "campaign"
}

func runID(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()

	campaignRoot, err := campaign.DetectCached(ctx)
	if err != nil {
		return err
	}

	cfg, err := config.LoadCampaignConfig(ctx, campaignRoot)
	if err != nil {
		return err
	}

	_, err = fmt.Fprintln(cmd.OutOrStdout(), cfg.ID)
	return err
}
