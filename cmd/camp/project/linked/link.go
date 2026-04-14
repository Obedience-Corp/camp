package linked

import "github.com/spf13/cobra"

// NewLinkCommand builds the canonical linked-project add command.
func NewLinkCommand(newResolver CampaignResolverFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <path>",
		Short: "Link an existing local project into a campaign",
		Long: `Link an existing local directory into a campaign without cloning it.

Linked projects are added as symlinks under projects/ and receive a local
.camp marker so camp can recover campaign context from inside the external
workspace later.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or place a bare --campaign
after the path to pick a registered target campaign interactively.

Examples:
  camp project link ~/code/my-project
  camp project link ~/code/my-project --name backend
  camp project link ~/code/my-project --campaign platform
  camp project link ~/code/my-project --campaign`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			linkPath := args[0]

			name, _ := cmd.Flags().GetString("name")
			path, _ := cmd.Flags().GetString("path")
			targetCampaign, _ := cmd.Flags().GetString("campaign")

			campaignResolver := newResolver(cmd.ErrOrStderr(), "camp project link <path> --campaign <name>")
			_, root, err := campaignResolver.Resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
			if err != nil {
				return err
			}

			result, err := Add(ctx, root, linkPath, name, path)
			if err != nil {
				return err
			}

			PrintResult(result)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("name", "n", "", "Override project name (defaults to directory name)")
	flags.StringP("path", "p", "", "Override destination path (defaults to projects/<name>)")
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}
