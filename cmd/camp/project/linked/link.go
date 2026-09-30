package linked

import (
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/spf13/cobra"
)

type linkFlags struct {
	name        string
	campaign    string
	noCommit    bool
	yes         bool
	interactive bool
	// forceCamp is set when a bare --campaign asks the user to choose.
	forceCamp bool
}

// NewLinkCommand builds the canonical linked-project add command.
func NewLinkCommand(newResolver CampaignResolverFactory) *cobra.Command {
	var flags linkFlags
	cmd := &cobra.Command{
		Use:   "link [path]",
		Short: "Link an existing local project into a camp",
		Long: `Link an existing local directory into a camp.

The folder stays where it is. Camp adds a shortcut at projects/<name>
and a .camp file in that folder. This is not a git submodule.

In a terminal, paste or type a path, or move through folders.
Enter links the folder. Tab opens a folder. Then name it, choose
the camp, and confirm before anything is written. Pass --yes,
or run the command without a terminal, to link immediately.

Inside a camp, that camp is selected for you. Outside a camp, the
browser asks which camp to use. --campaign <name-or-id> skips that
choice. A bare --campaign always asks.

Examples:
  camp project link
  camp project link ~/code/my-project
  camp project link ~/code/my-project --yes
  camp project link --campaign platform
  camp project link ~/code/my-project --name backend`,
		Args: validateLinkArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			targetCampaign, _ := cmd.Flags().GetString("campaign")
			flags.forceCamp = targetCampaign == NoOptCampaign
			flags.name, _ = cmd.Flags().GetString("name")
			flags.noCommit, _ = cmd.Flags().GetBool("no-commit")
			flags.yes, _ = cmd.Flags().GetBool("yes")
			flags.interactive, _ = cmd.Flags().GetBool("interactive")
			flags.campaign, args = normalizeLinkCampaignArgs(args, targetCampaign)

			if linkUsesTUI(flags, stdoutIsTTY()) {
				if !stdoutIsTTY() {
					return camperrors.New("project link needs a terminal; pass a path to link without one")
				}
				return runLinkTUI(cmd, args, flags, newResolver)
			}
			return runLinkImmediate(cmd, args, flags, newResolver)
		},
	}

	flagset := cmd.Flags()
	flagset.StringP("name", "n", "", "Override project name (defaults to directory name)")
	flagset.StringP("campaign", "c", "", "Target camp by name or ID; defaults to current camp or interactive picker")
	flagset.Bool("no-commit", false, "Skip automatic git commit")
	flagset.Bool("yes", false, "Link immediately without the folder screen")
	flagset.BoolP("interactive", "i", false, "Open the folder screen")
	flagset.Lookup("campaign").NoOptDefVal = NoOptCampaign

	return cmd
}

// linkUsesTUI reports whether this invocation should open the folder screen.
// --yes stays on the scripted path. -i forces the screen. Otherwise an
// interactive terminal opens it.
func linkUsesTUI(flags linkFlags, isTTY bool) bool {
	if flags.yes {
		return false
	}
	if flags.interactive {
		return true
	}
	return isTTY
}

func runLinkImmediate(cmd *cobra.Command, args []string, flags linkFlags, newResolver CampaignResolverFactory) error {
	ctx := cmd.Context()
	campaignResolver := newResolver(cmd.ErrOrStderr(), "camp project link [path] --campaign <name>")
	cfg, root, err := campaignResolver.Resolve(ctx, flags.campaign, cmd.Flags().Changed("campaign"))
	if err != nil {
		return err
	}

	linkPath, err := resolveLinkSourcePath(root, args)
	if err != nil {
		return err
	}

	result, err := Add(ctx, root, linkPath, flags.name)
	if err != nil {
		return err
	}

	PrintResult(result)
	if !flags.noCommit {
		commitResult := CommitLink(ctx, cfg, root, result.Path, result.Name)
		if commitResult.Message != "" {
			fmt.Printf("  %s\n", commitResult.Message)
		}
	}
	return nil
}
