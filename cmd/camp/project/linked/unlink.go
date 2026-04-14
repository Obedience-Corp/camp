package linked

import (
	"fmt"

	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// NewUnlinkCommand builds the canonical linked-project remove command.
func NewUnlinkCommand(newResolver CampaignResolverFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "unlink [name]",
		Short: "Unlink a linked project from a campaign",
		Long: `Remove a linked project symlink from a campaign without touching the
external workspace contents.

If name is omitted, the current linked project is inferred from the working
directory.

Use this for linked workspaces added with 'camp project link'. This command
removes the symlink entry from projects/ and cleans up the linked repo's local
.camp marker when it belongs to the selected campaign.

If you're already inside a campaign, that campaign is used by default.
Outside a campaign, use --campaign <name-or-id> or a bare --campaign to
pick a registered target campaign interactively.

Examples:
  camp project unlink
  camp project unlink my-project
  camp project unlink my-project --campaign platform
  camp project unlink --campaign platform
  camp project unlink my-project --campaign
  camp project unlink my-project --dry-run`,
		Args: validateUnlinkArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()

			targetCampaign, _ := cmd.Flags().GetString("campaign")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			noCommit, _ := cmd.Flags().GetBool("no-commit")
			targetCampaign, args = normalizeUnlinkCampaignArgs(args, targetCampaign)

			campaignResolver := newResolver(cmd.ErrOrStderr(), "camp project unlink [name] --campaign <name>")
			cfg, root, err := campaignResolver.Resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
			if err != nil {
				return err
			}

			name, err := resolveUnlinkName(ctx, root, args)
			if err != nil {
				return err
			}

			result, err := projectsvc.Unlink(ctx, root, name, projectsvc.UnlinkOptions{DryRun: dryRun})
			if err != nil {
				return err
			}

			if dryRun {
				fmt.Println(ui.Warning("Dry run - would unlink:"))
				fmt.Println()
				fmt.Println(ui.KeyValue("  Project:", result.Name))
				fmt.Println(ui.KeyValue("  Path:", result.Path))
				if result.Target != "" {
					fmt.Println(ui.KeyValue("  Target:", result.Target))
				}
				return nil
			}

			fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Unlinked project: "+result.Name))
			fmt.Println()
			fmt.Println(ui.KeyValue("  Path:", result.Path))
			if result.Target != "" {
				fmt.Println(ui.KeyValue("  Target:", result.Target))
			}
			if !noCommit {
				commitResult := CommitUnlink(ctx, cfg, root, result.Path, result.Name)
				if commitResult.Message != "" {
					fmt.Printf("  %s\n", commitResult.Message)
				}
			}
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; omit value to pick interactively")
	flags.Bool("dry-run", false, "Show what would be done without making changes")
	flags.Bool("no-commit", false, "Skip automatic git commit")
	flags.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}
