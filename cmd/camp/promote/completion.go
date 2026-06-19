package promote

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/paths"
)

func completePromotable(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ctx := cmd.Context()
	cfg, campaignRoot, err := config.LoadCampaignConfigFromCwd(ctx)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	resolver := paths.NewResolverFromConfig(campaignRoot, cfg)
	pool, err := promotablePool(ctx, campaignRoot, resolver)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var ids []string
	for _, it := range pool {
		id := it.SourceID
		if id == "" {
			id = it.Key
		}
		if strings.HasPrefix(id, toComplete) {
			ids = append(ids, id)
		}
	}
	return ids, cobra.ShellCompDirectiveNoFileComp
}
