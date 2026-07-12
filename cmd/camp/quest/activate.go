//go:build dev

package quest

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/quest"
)

// QuestStatusJSONVersion is the schema tag for `quest status --json`.
const QuestStatusJSONVersion = "quest-status/v1alpha1"

var questUseCmd = &cobra.Command{
	Use:   "use <quest>",
	Short: "Activate a quest for the current terminal",
	Long: `Activate a quest as the working context for the current terminal.

This prints shell code that sets ` + quest.QuestEnvVar + ` for the current shell, so
child processes (including camp itself) inherit the quest. It is terminal-local:
other terminals in the same campaign keep their own context.

The shell wrapper installed by 'camp shell-init' evaluates this automatically.
To activate without the wrapper:

  eval "$(camp quest use billing --shell bash)"

Examples:
  camp quest use billing
  camp quest use billing --allow-dungeon`,
	Args:              cobra.ExactArgs(1),
	ValidArgsFunction: completeQuestSelector,
	RunE:              runQuestUse,
}

var questClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the current terminal's quest context",
	Long: `Clear the terminal-local quest context by unsetting ` + quest.QuestEnvVar + `.

The shell wrapper installed by 'camp shell-init' evaluates this automatically.
Without the wrapper:

  eval "$(camp quest clear --shell bash)"`,
	Args: cobra.NoArgs,
	RunE: runQuestClear,
}

var questStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the active terminal quest context",
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Read-only terminal quest context with --json",
	},
	Args: cobra.NoArgs,
	RunE: jsoncontract.RunE(QuestStatusJSONVersion, func() bool { return questStatusJSON }, runQuestStatus),
}

var questStatusJSON bool

func init() {
	Cmd.AddCommand(questUseCmd, questClearCmd, questStatusCmd)
	questUseCmd.Flags().String("shell", "", "Shell dialect: posix (bash/zsh) or fish (default posix)")
	questUseCmd.Flags().Bool("allow-dungeon", false, "Allow activating a completed or archived quest")
	questClearCmd.Flags().String("shell", "", "Shell dialect: posix (bash/zsh) or fish (default posix)")
	questStatusCmd.Flags().BoolVar(&questStatusJSON, "json", false, "Emit JSON output")
	questStatusCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestStatusJSONVersion, func() bool { return questStatusJSON }))
}

func runQuestUse(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	dialect, err := quest.ParseShellDialect(mustFlagString(cmd, "shell"))
	if err != nil {
		return err
	}
	allowDungeon, _ := cmd.Flags().GetBool("allow-dungeon")

	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}
	q, err := qctx.service.Find(ctx, args[0])
	if err != nil {
		return err
	}
	// Emit shell code only after validation succeeds; on failure stdout stays
	// empty so the shell wrapper never evals a partial result.
	if q.Status.InDungeon() && !allowDungeon {
		return camperrors.Wrapf(camperrors.ErrInvalidInput,
			"quest %q is %s; pass --allow-dungeon to activate it", q.Name, q.Status)
	}

	fmt.Fprintln(cmd.OutOrStdout(), quest.RenderActivate(dialect, q.ID))
	fmt.Fprintf(cmd.ErrOrStderr(), "Activated quest for this terminal: %s (%s)\n", q.Name, q.ID)
	return nil
}

func runQuestClear(cmd *cobra.Command, _ []string) error {
	dialect, err := quest.ParseShellDialect(mustFlagString(cmd, "shell"))
	if err != nil {
		return err
	}
	fmt.Fprintln(cmd.OutOrStdout(), quest.RenderClear(dialect))
	fmt.Fprintln(cmd.ErrOrStderr(), "Cleared terminal quest context.")
	return nil
}

type questStatusPayload struct {
	SchemaVersion string       `json:"schema_version"`
	CampaignRoot  string       `json:"campaign_root"`
	Active        bool         `json:"active"`
	Source        string       `json:"source,omitempty"`
	RawEnv        string       `json:"raw_env,omitempty"`
	Valid         bool         `json:"valid"`
	Reason        string       `json:"reason,omitempty"`
	Quest         *quest.Quest `json:"quest,omitempty"`
}

func runQuestStatus(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	qctx, err := loadQuestCommandContext(ctx, true)
	if err != nil {
		return err
	}

	raw := strings.TrimSpace(os.Getenv(quest.QuestEnvVar))
	payload := questStatusPayload{SchemaVersion: QuestStatusJSONVersion, CampaignRoot: qctx.campaignRoot}
	switch {
	case raw == "":
		payload.Valid = true
	default:
		payload.Active = true
		payload.Source = quest.QuestEnvVar
		payload.RawEnv = raw
		q, findErr := qctx.service.Find(ctx, raw)
		if findErr != nil {
			if !errors.Is(findErr, quest.ErrQuestNotFound) {
				return findErr
			}
			payload.Valid = false
			payload.Reason = "quest not found"
		} else {
			payload.Valid = true
			item := q.Clone()
			item.Path = quest.RelativePath(qctx.campaignRoot, q.Path)
			payload.Quest = item
		}
	}

	if questStatusJSON {
		enc := json.NewEncoder(cmd.OutOrStdout())
		enc.SetIndent("", "  ")
		return enc.Encode(payload)
	}
	printQuestStatusHuman(cmd.OutOrStdout(), payload)
	return nil
}

func printQuestStatusHuman(w io.Writer, p questStatusPayload) {
	switch {
	case !p.Active:
		fmt.Fprintln(w, "No terminal quest active.")
		fmt.Fprintln(w, "\nUse:")
		fmt.Fprintln(w, "  camp quest use <quest>")
	case !p.Valid:
		fmt.Fprintln(w, "Active quest is invalid:")
		fmt.Fprintf(w, "  %s=%s\n", quest.QuestEnvVar, p.RawEnv)
		fmt.Fprintf(w, "\nReason:\n  %s\n", p.Reason)
		fmt.Fprintln(w, "\nUse:")
		fmt.Fprintln(w, "  camp quest clear")
	default:
		fmt.Fprintln(w, "Active quest for this terminal:")
		fmt.Fprintf(w, "  %s (%s)\n", p.Quest.Name, p.Quest.ID)
		fmt.Fprintf(w, "\nSource:\n  %s\n", p.Source)
		if p.Quest.Path != "" {
			fmt.Fprintf(w, "\nQuest root:\n  %s\n", p.Quest.Path)
		}
	}
}

func mustFlagString(cmd *cobra.Command, name string) string {
	v, _ := cmd.Flags().GetString(name)
	return v
}
