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
		Long: `Link an existing local directory into a campaign.

If path is omitted, camp links the current working directory.

If you're already inside a campaign, camp uses that campaign automatically.
If you're outside a campaign in an interactive terminal, camp opens a picker
so you can choose a registered campaign. Use --campaign <name-or-id> to skip
the picker or for non-interactive scripts.

This creates a symlink at projects/<name> and writes .camp with the selected
campaign ID.

Examples:
  camp project link                          # Link current directory
  camp project link ~/code/my-project        # Link another directory
  camp project link --campaign platform      # Link current directory to a specific campaign
  camp project link ~/code/my-project --campaign platform
  camp project link ~/code/my-project --name backend`,
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
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; defaults to current campaign or interactive picker")
	flags.Bool("no-commit", false, "Skip automatic git commit")
	flags.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}
