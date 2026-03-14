//go:build dev

package main

import (
	"encoding/json"
	"os"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
)

var questShowCmd = &cobra.Command{
	Use:   "show <quest>",
	Short: "Show quest metadata",
	Long: `Show quest metadata in human-readable, JSON, or raw YAML form.

Examples:
  camp quest show qst_default
  camp quest show runtime-hardening --json
  camp quest show runtime-hardening --yaml`,
	Args: cobra.ExactArgs(1),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive quest metadata display",
	},
	RunE: runQuestShow,
}

func init() {
	questCmd.AddCommand(questShowCmd)
	questShowCmd.Flags().Bool("json", false, "Output JSON")
	questShowCmd.Flags().Bool("yaml", false, "Output raw YAML")
	questShowCmd.ValidArgsFunction = completeQuestSelector
}

func runQuestShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	jsonOut, _ := cmd.Flags().GetBool("json")
	yamlOut, _ := cmd.Flags().GetBool("yaml")
	if jsonOut && yamlOut {
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
	case jsonOut:
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(q)
	default:
		outputQuestShow(qctx, q)
		return nil
	}
}
