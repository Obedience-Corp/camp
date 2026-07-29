//go:build dev

package audit

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/audit"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/git"
	"github.com/Obedience-Corp/camp/internal/ui"
	"github.com/Obedience-Corp/camp/pkg/ledgerkit"
)

func init() {
	Cmd.AddCommand(newReconcileCommand())
	Cmd.AddCommand(newRepairCommand())
}

func newReconcileCommand() *cobra.Command {
	var (
		apply   bool
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "reconcile",
		Short: "Fill ledger gaps from state files (opt-in write)",
		Long: `Derive the events implied by campaign state files (intent statuses and
festival status histories), diff them against the ledger, and report the gaps -
facts the ledger does not yet capture. This covers users who never commit at all.

By default this is a dry run. Pass --apply to append the missing facts as
reconciled events (idempotent: reconciled ids are content-derived, so re-running
does not duplicate).`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Dry-run reconciliation is read-only; --apply is an explicit opt-in write",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, campRoot, err := config.LoadCampaignConfigFromCwd(cmd.Context())
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}
			gaps, err := audit.Reconcile(cmd.Context(), campRoot, cfg.ID)
			if err != nil {
				return err
			}
			written := 0
			if apply && len(gaps) > 0 {
				if written, err = audit.Apply(cmd.Context(), campRoot, gaps); err != nil {
					return err
				}
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion string `json:"schema_version"`
					Gaps          int    `json:"gaps"`
					Applied       int    `json:"applied"`
				}{"camp-audit-reconcile/v1", len(gaps), written})
			}
			w := cmd.OutOrStdout()
			if len(gaps) == 0 {
				_, err := fmt.Fprintln(w, ui.Success("Ledger is consistent with state files: no gaps."))
				return err
			}
			if apply {
				_, err := fmt.Fprintf(w, "%s Reconciled %d gap(s) into the ledger.\n", ui.SuccessIcon(), written)
				return err
			}
			_, err = fmt.Fprintf(w, "%s %d state-file fact(s) are not yet in the ledger. Re-run with --apply to record them as reconciled events.\n",
				ui.InfoIcon(), len(gaps))
			return err
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "append the missing facts as reconciled events")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}

func newRepairCommand() *cobra.Command {
	in := audit.RepairInput{}
	cmd := &cobra.Command{
		Use:   "repair --sha <sha> (--workitem <id> | --festival <id>) --why <reason>",
		Short: "Attribute a commit to a workitem or festival after the fact",
		Long: `Attribute an already-landed commit to a workitem or festival by appending a
repaired event. This never rewrites git history; it only records the attribution
in the ledger (D004). Use it to claim an untagged commit surfaced by
'camp audit doctor' for a piece of work.`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Appends attribution; requires --why and is write-idempotent on re-run",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, campRoot, err := config.LoadCampaignConfigFromCwd(cmd.Context())
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}
			in.CampaignID = cfg.ID
			if in.Repo == "" {
				in.Repo = "campaign-root"
			}
			in.Actor = ledgerkit.Actor{Type: ledgerkit.ActorHuman, Name: git.GetUserName(cmd.Context())}
			ev, err := audit.BuildRepair(in)
			if err != nil {
				return err
			}
			// Write-idempotent: content-derived rp_ ids skip when already present.
			present, perr := audit.EventIDPresent(cmd.Context(), campRoot, ev.ID)
			if perr != nil {
				return camperrors.Wrap(perr, "check existing repair")
			}
			if present {
				_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s Already repaired: %s@%s → %s (no new event)\n",
					ui.InfoIcon(), in.Repo, shortSHA(in.SHA), scopeLabel(in))
				return err
			}
			if _, err := audit.Apply(cmd.Context(), campRoot, []*ledgerkit.Event{ev}); err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "%s Repaired: attributed %s@%s to %s\n",
				ui.SuccessIcon(), in.Repo, shortSHA(in.SHA), scopeLabel(in))
			return err
		},
	}
	f := cmd.Flags()
	f.StringVar(&in.SHA, "sha", "", "commit sha to attribute (required)")
	f.StringVar(&in.Repo, "repo", "", "evidence repo label (default: campaign-root)")
	f.StringVar(&in.Workitem, "workitem", "", "workitem to attribute the commit to")
	f.StringVar(&in.Festival, "festival", "", "festival to attribute the commit to")
	f.StringVar(&in.Why, "why", "", "reason for the attribution (required)")
	_ = cmd.MarkFlagRequired("why")
	return cmd
}

func scopeLabel(in audit.RepairInput) string {
	if in.Workitem != "" {
		return "workitem " + in.Workitem
	}
	return "festival " + in.Festival
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
