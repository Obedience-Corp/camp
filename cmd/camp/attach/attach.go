// Package attach provides the camp attach / camp detach top-level commands
// for binding non-project directories to a campaign via the .camp marker.
package attach

import (
	"context"
	"fmt"
	"io"

	"github.com/Obedience-Corp/camp/internal/attach"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/spf13/cobra"
)

// CampaignResolver selects a target campaign for attach, mirroring the
// linked-project resolver contract.
type CampaignResolver interface {
	Resolve(ctx context.Context, targetCampaign string, targetChanged bool) (*config.CampaignConfig, string, error)
}

// CampaignResolverFactory creates a campaign resolver for a command instance.
type CampaignResolverFactory func(stderr io.Writer, usageLine string) CampaignResolver

// NoOptCampaign mirrors the linked-project NoOptDefVal so a bare --campaign
// triggers the picker in interactive shells.
const NoOptCampaign = "\x00pick"

// NewAttachCommand builds the camp attach command.
func NewAttachCommand(newResolver CampaignResolverFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "attach <path>",
		Short:   "Attach an external directory to a campaign",
		GroupID: "campaign",
		Long: `Attach a non-project directory to a campaign by writing a .camp marker.

The user manages the symlink (if any). camp attach only writes the marker at
the resolved target so commands run from inside that directory know which
campaign owns it.

If the target is reached through a symlink, camp follows it once and writes
the marker at the final directory.

Examples:
  camp attach ai_docs/examples/external-repo
  camp attach ~/scratch/notes-link
  camp attach /abs/path/to/dir --campaign platform`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			input := args[0]

			targetCampaign, _ := cmd.Flags().GetString("campaign")
			force, _ := cmd.Flags().GetBool("force")

			resolver := newResolver(cmd.ErrOrStderr(), "camp attach <path> --campaign <name>")
			cfg, root, err := resolver.Resolve(ctx, targetCampaign, cmd.Flags().Changed("campaign"))
			if err != nil {
				return err
			}
			if cfg == nil {
				return camperrors.Wrap(camperrors.ErrNotFound, "could not resolve target campaign")
			}

			result, err := attach.Attach(ctx, root, cfg.ID, input, attach.Options{Force: force})
			if err != nil {
				return err
			}

			printAttachResult(result, cfg.Name)
			return nil
		},
	}

	flags := cmd.Flags()
	flags.StringP("campaign", "c", "", "Target campaign by name or ID; defaults to current campaign or interactive picker")
	flags.Bool("force", false, "Overwrite an existing attachment marker")
	flags.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}

// NewDetachCommand builds the camp detach command.
func NewDetachCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "detach <path>",
		Short:   "Remove the attachment marker from a directory",
		GroupID: "campaign",
		Long: `Remove the .camp attachment marker from the target directory.

Refuses on linked-project markers; use 'camp project unlink' for those.
The user-managed symlink (if any) is not modified.

Examples:
  camp detach ai_docs/examples/external-repo
  camp detach ~/scratch/notes-link`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			input := args[0]

			result, err := attach.Detach(ctx, input)
			if err != nil {
				return err
			}

			printDetachResult(result)
			return nil
		},
	}
	return cmd
}

func printAttachResult(r *attach.Result, campaignName string) {
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Attached to campaign: "+campaignName))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Target:", r.Target))
	if r.FollowedSymlink {
		fmt.Println(ui.KeyValue("  Input:", r.Input+" (followed symlink)"))
	}
	fmt.Println(ui.KeyValue("  Campaign ID:", r.CampaignID))
	if r.GitExcludeUpdated {
		fmt.Println(ui.KeyValue("  Git exclude:", "added .camp to .git/info/exclude"))
	}
	if r.GitExcludeWarning != "" {
		fmt.Printf("%s %s\n", ui.WarningIcon(), ui.Warning("could not update .git/info/exclude: "+r.GitExcludeWarning))
	}
	fmt.Println()
	fmt.Println(ui.Dim("  Commands run from inside the target now resolve to this campaign."))
}

func printDetachResult(r *attach.Result) {
	fmt.Printf("%s %s\n", ui.SuccessIcon(), ui.Success("Detached attachment"))
	fmt.Println()
	fmt.Println(ui.KeyValue("  Target:", r.Target))
	if r.FollowedSymlink {
		fmt.Println(ui.KeyValue("  Input:", r.Input+" (followed symlink)"))
	}
	if r.GitExcludeUpdated {
		fmt.Println(ui.KeyValue("  Git exclude:", "removed .camp from .git/info/exclude"))
	}
	if r.GitExcludeWarning != "" {
		fmt.Printf("%s %s\n", ui.WarningIcon(), ui.Warning("could not update .git/info/exclude: "+r.GitExcludeWarning))
	}
}
