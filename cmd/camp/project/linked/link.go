package linked

import (
	"fmt"

	"github.com/spf13/cobra"
)

// NewLinkCommand builds the canonical linked-project add command.
func NewLinkCommand(newResolver CampaignResolverFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link [path]",
		Short: "Link an existing local project into a campaign",
		Long: `Link an existing local directory into a campaign without cloning it.

Linked projects are added as symlinks under projects/ and receive a local
.camp marker so camp can recover campaign context from inside the external
workspace later.

If path is omitted, the current working directory is linked.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
pick a registered target campaign interactively.

Examples:
  camp project link
  camp project link --campaign platform
  camp project link ~/code/my-project
  camp project link ~/code/my-project --name backend
  camp project link ~/code/my-project --campaign platform
  camp project link ~/code/my-project --campaign`,
		Args: validateLinkArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			name, _ := cmd.Flags().GetString("name")
			targetCampaign, _ := cmd.Flags().GetString("campaign")
			noCommit, _ := cmd.Flags().GetBool("no-commit")
			targetCampaign, args = normalizeLinkCampaignArgs(args, targetCampaign)

			campaignResolver := newResolver(cmd.ErrOrStderr(), "camp project link [path] --campaign <name>")
			cfg, root, err := campaignResolver.Resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
			if err != nil {
				return err
			}

			linkPath, err := resolveLinkSourcePath(root, args)
			if err != nil {
				return err
			}

			result, err := Add(ctx, root, linkPath, name)
			if err != nil {
				return err
			}

			PrintResult(result)
			if !noCommit {
				commitResult := CommitLink(ctx, cfg, root, result.Path, result.Name)
				if commitResult.Message != "" {
					fmt.Printf("  %s\n", commitResult.Message)
				}
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("name", "n", "", "Override project name (defaults to directory name)")
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Bool("no-commit", false, "Skip automatic git commit")
	flags.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}
