//go:build dev

package quest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/pathutil"
	"github.com/Obedience-Corp/camp/internal/quest"
	"github.com/Obedience-Corp/camp/internal/quest/workspace"
)

const QuestWorkspaceJSONVersion = workspace.SchemaVersion

var (
	questWorkspaceShowJSON     bool
	questWorkspaceValidateJSON bool
	questWorkspacePushJSON     bool
	questWorkspacePushFile     string
)

var questWorkspaceCmd = &cobra.Command{
	Use:   "workspace",
	Short: "Show, validate, or push quest workspace layouts",
	Long: `Manage the Camp-owned quest workspace layout contract (quest-workspace/v1alpha1).

Layouts live at .campaign/quests/<quest>/ui-state.json (festival-app version 2
shape: workspaceTabs + paneTree). Offline push appends or replaces agent-owned
tabs only; user tabs and unknown metadata are preserved. Requires neither the
desktop app nor the camp-app plugin.

Examples:
  camp quest workspace show my-quest --json
  camp quest workspace validate my-quest
  camp quest workspace push my-quest --file tab.json --json
  cat tab.json | camp quest workspace push my-quest --json`,
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Offline agent workspace composition without GUI",
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return cmd.Help()
	},
}

var questWorkspaceShowCmd = &cobra.Command{
	Use:   "show <quest>",
	Short: "Show the quest workspace layout document",
	Args:  jsoncontract.Args(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceShowJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive workspace layout read",
	},
	RunE: jsoncontract.RunE(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceShowJSON }, runQuestWorkspaceShow),
}

var questWorkspaceValidateCmd = &cobra.Command{
	Use:   "validate <quest>",
	Short: "Validate the quest workspace layout document",
	Args:  jsoncontract.Args(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceValidateJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Non-interactive workspace layout validation",
	},
	RunE: jsoncontract.RunE(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceValidateJSON }, runQuestWorkspaceValidate),
}

var questWorkspacePushCmd = &cobra.Command{
	Use:   "push <quest>",
	Short: "Append or replace an agent-owned workspace tab",
	Long: `Push one workspace tab from --file or stdin into the quest ui-state.json.

The entire tab is validated before any write. Camp stamps origin=agent and
canonicalizes permitted camp-relative file/workitem paths. Only a previously
agent-owned tab ID may be replaced; user-owned IDs are rejected. Repeating the
same agent tab ID is idempotent (replace, not duplicate).`,
	Args: jsoncontract.Args(QuestWorkspaceJSONVersion, func() bool { return questWorkspacePushJSON }, cobra.ExactArgs(1)),
	Annotations: map[string]string{
		"agent_allowed": "true",
		"agent_reason":  "Offline agent tab composition",
	},
	RunE: jsoncontract.RunE(QuestWorkspaceJSONVersion, func() bool { return questWorkspacePushJSON }, runQuestWorkspacePush),
}

func init() {
	Cmd.AddCommand(questWorkspaceCmd)
	questWorkspaceCmd.AddCommand(questWorkspaceShowCmd, questWorkspaceValidateCmd, questWorkspacePushCmd)

	questWorkspaceShowCmd.Flags().BoolVar(&questWorkspaceShowJSON, "json", false, "Output JSON")
	questWorkspaceShowCmd.ValidArgsFunction = completeQuestSelector
	questWorkspaceShowCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceShowJSON }))

	questWorkspaceValidateCmd.Flags().BoolVar(&questWorkspaceValidateJSON, "json", false, "Output JSON")
	questWorkspaceValidateCmd.ValidArgsFunction = completeQuestSelector
	questWorkspaceValidateCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestWorkspaceJSONVersion, func() bool { return questWorkspaceValidateJSON }))

	questWorkspacePushCmd.Flags().BoolVar(&questWorkspacePushJSON, "json", false, "Output JSON")
	questWorkspacePushCmd.Flags().StringVar(&questWorkspacePushFile, "file", "", "Path to push spec JSON (default: stdin)")
	questWorkspacePushCmd.ValidArgsFunction = completeQuestSelector
	questWorkspacePushCmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(QuestWorkspaceJSONVersion, func() bool { return questWorkspacePushJSON }))
}

func runQuestWorkspaceShow(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, q, uiPath, err := resolveQuestWorkspace(ctx, args[0])
	if err != nil {
		return err
	}
	store := &workspace.Store{}
	raw, gen, err := store.Read(uiPath)
	if err != nil {
		return err
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := workspace.ValidateDocument(qctx.campaignRoot, raw); err != nil {
			return err
		}
	}
	rel, err := pathutil.RelativeToRoot(qctx.campaignRoot, uiPath)
	if err != nil {
		return camperrors.Wrap(err, "relativizing ui-state path")
	}
	if questWorkspaceShowJSON {
		return encodeJSON(workspace.ShowResult{
			SchemaVersion: QuestWorkspaceJSONVersion,
			CampaignRoot:  qctx.campaignRoot,
			QuestID:       q.ID,
			QuestSlug:     q.Slug,
			Path:          filepath.ToSlash(rel),
			Generation:    gen,
			Document:      json.RawMessage(raw),
		})
	}
	cmd.Printf("Quest:       %s (%s)\n", q.Name, q.ID)
	cmd.Printf("Path:        %s\n", filepath.ToSlash(rel))
	cmd.Printf("Generation:  %s\n", gen)
	cmd.Printf("\n%s\n", string(raw))
	return nil
}

func runQuestWorkspaceValidate(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, q, uiPath, err := resolveQuestWorkspace(ctx, args[0])
	if err != nil {
		return err
	}
	store := &workspace.Store{}
	raw, gen, err := store.Read(uiPath)
	if err != nil {
		return err
	}
	if err := workspace.ValidateDocument(qctx.campaignRoot, raw); err != nil {
		return err
	}
	rel, err := pathutil.RelativeToRoot(qctx.campaignRoot, uiPath)
	if err != nil {
		return camperrors.Wrap(err, "relativizing ui-state path")
	}
	if questWorkspaceValidateJSON {
		return encodeJSON(workspace.ValidateResult{
			SchemaVersion: QuestWorkspaceJSONVersion,
			CampaignRoot:  qctx.campaignRoot,
			QuestID:       q.ID,
			QuestSlug:     q.Slug,
			Path:          filepath.ToSlash(rel),
			Valid:         true,
			Generation:    gen,
		})
	}
	cmd.Printf("valid: %s (%s)\n", q.Slug, filepath.ToSlash(rel))
	return nil
}

func runQuestWorkspacePush(cmd *cobra.Command, args []string) error {
	ctx := cmd.Context()
	qctx, q, uiPath, err := resolveQuestWorkspace(ctx, args[0])
	if err != nil {
		return err
	}
	specBytes, err := readPushSpec(cmd, questWorkspacePushFile)
	if err != nil {
		return err
	}
	var spec workspace.PushSpec
	if err := json.Unmarshal(specBytes, &spec); err != nil {
		return camperrors.Wrap(camperrors.ErrInvalidInput, "push spec is not valid JSON")
	}

	store := &workspace.Store{}
	merged, gen, err := store.Push(ctx, qctx.campaignRoot, uiPath, &spec)
	if err != nil {
		return err
	}

	rel, err := pathutil.RelativeToRoot(qctx.campaignRoot, uiPath)
	if err != nil {
		return camperrors.Wrap(err, "relativizing ui-state path")
	}
	if questWorkspacePushJSON {
		return encodeJSON(workspace.PushResult{
			SchemaVersion: QuestWorkspaceJSONVersion,
			CampaignRoot:  qctx.campaignRoot,
			QuestID:       q.ID,
			QuestSlug:     q.Slug,
			Path:          filepath.ToSlash(rel),
			TabID:         merged.TabID,
			Action:        merged.Action,
			Generation:    gen,
			Document:      json.RawMessage(merged.Document),
		})
	}
	cmd.Printf("%s agent tab %s on %s\n", merged.Action, merged.TabID, q.Slug)
	cmd.Printf("generation: %s\n", gen)
	return nil
}

func resolveQuestWorkspace(ctx context.Context, identifier string) (*questCommandContext, *quest.Quest, string, error) {
	qctx, err := loadQuestCommandContext(ctx, false)
	if err != nil {
		return nil, nil, "", err
	}
	q, err := qctx.service.Find(ctx, identifier)
	if err != nil {
		return nil, nil, "", err
	}
	return qctx, q, workspace.UIStatePath(q.Path), nil
}

func readPushSpec(cmd *cobra.Command, file string) ([]byte, error) {
	file = strings.TrimSpace(file)
	if file != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, camperrors.Wrap(err, "read push --file")
		}
		return data, nil
	}
	stat, err := os.Stdin.Stat()
	if err != nil {
		return nil, camperrors.Wrap(err, "stat stdin")
	}
	if (stat.Mode() & os.ModeCharDevice) != 0 {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "push requires --file or stdin")
	}
	data, err := io.ReadAll(cmd.InOrStdin())
	if err != nil {
		return nil, camperrors.Wrap(err, "read push stdin")
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, camperrors.Wrap(camperrors.ErrInvalidInput, "push spec is empty")
	}
	return data, nil
}

func encodeJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
