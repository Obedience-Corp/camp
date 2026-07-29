//go:build dev

package audit

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/audit"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/ui"
)

func init() {
	Cmd.AddCommand(newBackfillCommand())
}

func newBackfillCommand() *cobra.Command {
	var (
		apply   bool
		jsonOut bool
	)
	cmd := &cobra.Command{
		Use:   "backfill",
		Short: "Derive a source:backfill event stream from history (opt-in write)",
		Long: `Derive ledger events from a campaign's existing history - tagged commits across
linked repos, intent frontmatter, and festival status histories - so a pre-ledger
campaign (or the pre-ledger history of this one) renders on the same timeline as
new activity.

Backfill is optional and never required. It is idempotent and live-wins: a fact
already captured live or by a prior backfill is skipped, so consecutive runs
produce zero new events. Dry-run by default; --apply writes the source:backfill
events into the standard shard layout.`,
		Args: cobra.NoArgs,
		Annotations: map[string]string{
			"agent_allowed": "true",
			"agent_reason":  "Dry-run backfill is read-only; --apply is an explicit opt-in write",
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			cfg, campRoot, err := config.LoadCampaignConfigFromCwd(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a campaign directory")
			}
			targets, err := scanTargets(ctx, campRoot)
			if err != nil {
				return err
			}
			repos := make([]audit.RepoTarget, 0, len(targets))
			for _, t := range targets {
				repos = append(repos, audit.RepoTarget{Label: t.label, Path: t.path})
			}
			res, err := audit.Backfill(ctx, campRoot, cfg.ID, repos)
			if err != nil {
				return err
			}
			written := 0
			if apply && len(res.Events) > 0 {
				if written, err = audit.Apply(ctx, campRoot, res.Events); err != nil {
					return err
				}
			}
			if jsonOut {
				return json.NewEncoder(cmd.OutOrStdout()).Encode(struct {
					SchemaVersion string `json:"schema_version"`
					Derived       int    `json:"derived"`
					Skipped       int    `json:"already_captured"`
					New           int    `json:"new"`
					Applied       int    `json:"applied"`
				}{"camp-audit-backfill/v1", res.Derived, res.Skipped, len(res.Events), written})
			}
			w := cmd.OutOrStdout()
			if apply {
				_, err := fmt.Fprintf(w, "%s Backfilled %d event(s) (%d derived, %d already captured).\n",
					ui.SuccessIcon(), written, res.Derived, res.Skipped)
				return err
			}
			_, err = fmt.Fprintf(w, "%s %d event(s) would be backfilled (%d derived, %d already captured). Re-run with --apply to write them.\n",
				ui.InfoIcon(), len(res.Events), res.Derived, res.Skipped)
			return err
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "write the derived source:backfill events into the ledger")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	return cmd
}
