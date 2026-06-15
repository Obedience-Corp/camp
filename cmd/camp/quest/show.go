//go:build dev

package quest

import (
	"os"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
)

const QuestShowJSONVersion = "quest-show/v1alpha1"

var questShowJSON bool

var questShowCmd = &cobra.Command{
	Use:   "show <quest>",
	Short: "Show quest metadata",
	Long: `Show quest metadata in human-readable, JSON, or raw YAML form.

Examples:
  camp quest show qst_default
  camp quest show platform-launch --json
  camp quest show platform-launch --yaml`,
	Args: jsoncontract.Args(QuestShowJSONVersion, func() bool { return questShowJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest metadata display",
	},
	RunE: jsoncontract.RunE(QuestShowJSONVersion, func() bool { return questShowJSON }, runQuestShow),
}

func init() {
	Cmd.AddCommand(questShowCmd)
	questShowCmd.Flags().BoolVar(&questShowJSON, "json", false, "Output JSON")
	questShowCmd.Flags().Bool("yaml", false, "Output raw YAML")
	questShowCmd.ValidArgsFunction = completeQuestSelector
	questShowCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestShowJSONVersion, func() bool { return questShowJSON }))
}

func runQuestShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	yamlOut, _ := cmd.Flags().GetBool("yaml")
	if questShowJSON && yamlOut {
		return camperrors.New("use only one of --json or --yaml")
	}

	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return err
	}

	raw, q, err := qctx.service.ReadRaw(ctx, args[0])
	if err != nil {
		return err
	}

	switch {
	case yamlOut:
		_, err = os.Stdout.Write(raw)
		return err
	case questShowJSON:
		return outputQuestShowJSON(qctx, q)
	default:
		outputQuestShow(qctx, q)
		return nil
	}
}
