package quest

import (
	"encoding/json"
	"fmt"

	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/spf13/cobra"
)

const QuestDefaultJSONVersion = "quest-default/v1alpha1"

var questDefaultJSON bool

var questDefaultCmd = &cobra.Command{
	Use:   "default",
	Short: "Resolve or create an open default quest",
	Long: `Return the current open fallback quest, creating a fresh default when
previous defaults are paused, completed, or archived. Existing quests and their
workspaces stay unchanged. The new default has a distinct ID and is reused by
later calls, including concurrent calls. Merely naming a quest "default" does
not make it the fallback.

This command does not commit or open an editor. Read-only callers should use
camp quest list --json --no-scaffold instead.

Examples:
  camp quest default
  camp quest default --json`,
	Args: jsoncontract.Args(QuestDefaultJSONVersion, func() bool { return questDefaultJSON }, cobra.NoArgs),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Idempotent non-interactive default quest resolution",
	},
	RunE: jsoncontract.RunE(QuestDefaultJSONVersion, func() bool { return questDefaultJSON }, runQuestDefault),
}

func init() {
	Cmd.AddCommand(questDefaultCmd)
	questDefaultCmd.Flags().BoolVar(&questDefaultJSON, "json", false, "Output the resolved quest and whether it was created")
	questDefaultCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestDefaultJSONVersion, func() bool { return questDefaultJSON }))
}

func runQuestDefault(cmd *cobra.Command, _ []string) error {
	qctx, err := loadQuestCommandContext(cmd.Context(), false)
	if err != nil {
		return err
	}
	q, created, err := qctx.service.EnsureDefault(cmd.Context())
	if err != nil {
		return err
	}
	if created {
		emitQuestCreated(cmd, qctx, &quest.MutationResult{Quest: q})
	}
	if !questDefaultJSON {
		if created {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Created default quest %s (%s)\n", q.ID, q.Slug)
		} else {
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "Default quest %s (%s)\n", q.ID, q.Slug)
		}
		return err
	}
	item, err := questForJSON(qctx, q)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(cmd.OutOrStdout())
	enc.SetIndent("", "  ")
	return enc.Encode(struct {
		SchemaVersion string       `json:"schema_version"`
		CampaignRoot  string       `json:"campaign_root"`
		Quest         *quest.Quest `json:"quest"`
		Created       bool         `json:"created"`
	}{QuestDefaultJSONVersion, qctx.campaignRoot, item, created})
}
