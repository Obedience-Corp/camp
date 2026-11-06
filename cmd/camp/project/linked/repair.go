package linked

import (
	"encoding/json"
	"fmt"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	projectsvc "github.com/Obedience-Corp/camp/internal/project"
	"github.com/spf13/cobra"
)

// NewRepairCommand previews an explicit replacement for a broken project link.
func NewRepairCommand(newResolver CampaignResolverFactory) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repair <name>",
		Short: "Preview repair of a broken local project link",
		Long: `Inspect a broken projects/<name> link and possible local checkouts.

Discovery searches registered camps for checkouts with the same project name.
The preview shows each checkout's origin and existing .camp owner. A matching
folder name is only a discovery hint; select the intended checkout with --path.
Preview never changes the link, checkout, marker, or Git index. --apply
requires --path and replaces only a still-broken link; --undo restores the
saved old target. Repair changes a tracked symlink in the camp working tree
when that link is tracked. Neither action commits or changes checkout ownership.
Undo state is stored beside the local camp registry. The normal 'project link'
command creates a new shortcut and may attach the checkout; repair only
replaces an existing broken shortcut. See docs/guides/clone-sync-recovery.md.

Examples:
  camp project repair obey-voice
  camp project repair obey-voice --path ~/code/obey-voice
  camp project repair obey-voice --path ~/code/obey-voice --apply
  camp project repair obey-voice --undo
  camp project repair obey-voice --campaign obey-campaign --json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			campaignName, _ := cmd.Flags().GetString("campaign")
			candidatePath, _ := cmd.Flags().GetString("path")
			jsonOutput, _ := cmd.Flags().GetBool("json")
			apply, _ := cmd.Flags().GetBool("apply")
			undo, _ := cmd.Flags().GetBool("undo")
			if apply && undo {
				return camperrors.New("--apply and --undo cannot be combined")
			}
			if apply && candidatePath == "" {
				return camperrors.New("--apply requires --path /absolute/checkout")
			}
			if undo && candidatePath != "" {
				return camperrors.New("--undo does not accept --path")
			}
			resolver := newResolver(cmd.ErrOrStderr(), "camp project repair <name> --campaign <name>")
			_, root, err := resolver.Resolve(cmd.Context(), campaignName, cmd.Flags().Changed("campaign"))
			if err != nil {
				return err
			}
			if undo {
				state, err := projectsvc.UndoLinkRepair(cmd.Context(), root, args[0])
				if err != nil {
					return err
				}
				if jsonOutput {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(state)
				}
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "Restored projects/%s → %s\n", state.Name, state.OldTarget)
				return err
			}
			preview, err := projectsvc.PreviewLinkRepair(cmd.Context(), root, args[0], candidatePath)
			if err != nil {
				return err
			}
			if apply {
				state, err := projectsvc.ApplyLinkRepair(cmd.Context(), root, preview)
				if err != nil {
					return err
				}
				if jsonOutput {
					return json.NewEncoder(cmd.OutOrStdout()).Encode(state)
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), projectsvc.RepairStateDescription(state)); err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), "The camp working tree now contains the local link change; commit it only if intended for this machine.")
				return err
			}
			if jsonOutput {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(preview)
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Broken link: %s → %s\n", preview.Path, preview.OldTarget); err != nil {
				return err
			}
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Tracked file change on apply: %t\n", preview.TrackedChange); err != nil {
				return err
			}
			for _, candidate := range preview.Candidates {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Candidate: %s\n    Origin: %s\n    Identity: %s\n    Owner: %s (%s)\n", candidate.Path, candidate.Origin, candidate.OriginKey, candidate.OwnerName, candidate.OwnerID); err != nil {
					return err
				}
			}
			for _, warning := range preview.Warnings {
				if _, err := fmt.Fprintf(cmd.OutOrStdout(), "  Warning: %s\n", warning); err != nil {
					return err
				}
			}
			_, err = fmt.Fprintln(cmd.OutOrStdout(), projectsvc.RepairSelectionHint(preview))
			return err
		},
	}
	cmd.Flags().StringP("campaign", "c", "", "Target camp by registered name or ID")
	cmd.Flags().String("path", "", "Inspect an explicit local checkout")
	cmd.Flags().Bool("json", false, "Output preview as JSON")
	cmd.Flags().Bool("apply", false, "Apply the selected repair without committing")
	cmd.Flags().Bool("undo", false, "Restore the saved original link target")
	return cmd
}
