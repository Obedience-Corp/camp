package fresh

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/Obedience-Corp/camp/internal/campaign"
	"github.com/Obedience-Corp/camp/internal/config"
	camperrors "github.com/Obedience-Corp/camp/internal/errors"
	"github.com/Obedience-Corp/camp/internal/jsoncontract"
	"github.com/Obedience-Corp/camp/internal/project"
)

type freshSetResult struct {
	SchemaVersion string `json:"schema_version"`
	Key           string `json:"key"`
	Project       string `json:"project"`
	Action        string `json:"action"`
	Changed       bool   `json:"changed"`
	Outcome       string `json:"outcome"`
}

type freshSetInput struct {
	Key     string
	Action  string
	Value   string
	Project string
}

func newConfigureSetCommand() *cobra.Command {
	var (
		action      string
		value       string
		projectName string
		jsonOut     bool
	)

	cmd := &cobra.Command{
		Use:   "set <key>",
		Short: "Set a camp fresh workflow setting",
		Long: `Change a fresh.yaml settings key without opening the interactive TUI.

Keys:
  branch          working branch created after sync
  push_upstream   push the working branch with --set-upstream
  prune           prune merged branches (camp-wide)
  prune_remote    prune stale remote tracking refs (camp-wide)

Actions:
  inherit     clear the key (project inherits global; global restores the built-in)
  on / off    write an explicit bool
  no-branch   stay on the default branch
  branch      create a working branch; requires --value

prune and prune_remote are camp-wide. Pass them without --project.

Examples:
  camp fresh configure set prune --action off
  camp fresh configure set branch --action branch --value feat/next --project camp
  camp fresh configure set push_upstream --action inherit --project camp
  camp fresh configure set branch --action no-branch`,
		Args: jsoncontract.Args(JSONSchemaVersion, func() bool { return jsonOut }, cobra.ExactArgs(1)),
		RunE: jsoncontract.RunE(JSONSchemaVersion, func() bool { return jsonOut }, func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			campRoot, err := campaign.DetectCached(ctx)
			if err != nil {
				return camperrors.Wrap(err, "not in a camp")
			}
			if strings.TrimSpace(projectName) != "" {
				resolved, err := project.Resolve(ctx, campRoot, projectName)
				if err != nil {
					return err
				}
				projectName = resolved.Name
			}
			result, err := applyFreshSetting(ctx, campRoot, freshSetInput{
				Key:     args[0],
				Action:  action,
				Value:   value,
				Project: projectName,
			})
			if err != nil {
				return err
			}
			if jsonOut {
				return emitFreshSetJSON(cmd.OutOrStdout(), result)
			}
			fmt.Fprintln(cmd.OutOrStdout(), result.Outcome)
			return nil
		}),
	}

	cmd.SetFlagErrorFunc(jsoncontract.FlagErrorFunc(JSONSchemaVersion, func() bool { return jsonOut }))
	cmd.Flags().StringVar(&action, "action", "", "inherit, on, off, no-branch, or branch (required)")
	cmd.Flags().StringVar(&value, "value", "", "Branch name when --action branch")
	cmd.Flags().StringVar(&projectName, "project", "", "Project scope (default: global)")
	cmd.Flags().BoolVar(&jsonOut, "json", false, "emit a structured JSON result")
	_ = cmd.MarkFlagRequired("action")
	_ = cmd.RegisterFlagCompletionFunc("project", completeProjectName)

	return cmd
}

func emitFreshSetJSON(w io.Writer, result freshSetResult) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func applyFreshSetting(ctx context.Context, campRoot string, in freshSetInput) (freshSetResult, error) {
	key, ok := parseSettingKey(in.Key)
	if !ok {
		return freshSetResult{}, camperrors.NewValidation("key", "must be branch, push_upstream, prune, or prune_remote", nil)
	}
	action, ok := parseSettingAction(in.Action)
	if !ok {
		return freshSetResult{}, camperrors.NewValidation("action", "must be inherit, on, off, no-branch, or branch", nil)
	}
	if (key == freshSettingPrune || key == freshSettingPruneRemote) && in.Project != "" {
		return freshSetResult{}, camperrors.NewValidation(settingTitle(key), "is camp-wide; omit --project and set it on the global defaults", nil)
	}
	if err := validateSettingAction(key, action, in.Value); err != nil {
		return freshSetResult{}, err
	}

	cfg, err := config.LoadFreshConfig(ctx, campRoot)
	if err != nil {
		return freshSetResult{}, camperrors.Wrap(err, "loading fresh config")
	}
	step := settingStepFor(cfg, in.Project, key)
	result := freshSetResult{
		SchemaVersion: JSONSchemaVersion,
		Key:           settingTitle(key),
		Project:       in.Project,
		Action:        settingActionName(action),
	}

	if unchanged, notice := settingUnchanged(cfg, in.Project, step, action, strings.TrimSpace(in.Value)); unchanged {
		result.Outcome = notice
		return result, nil
	}

	outcome, err := writeFreshSetting(ctx, campRoot, in.Project, key, action, strings.TrimSpace(in.Value))
	if err != nil {
		return freshSetResult{}, err
	}
	result.Changed = true
	result.Outcome = outcome + " in " + workflowScopeLabel(in.Project)
	return result, nil
}

func validateSettingAction(key freshSettingKey, action freshSettingAction, value string) error {
	switch key {
	case freshSettingBranch:
		switch action {
		case freshSettingInherit, freshSettingNoBranch:
			return nil
		case freshSettingCustomBranch:
			if strings.TrimSpace(value) == "" {
				return camperrors.NewValidation("branch", "must not be empty; choose --action no-branch to stay on the default branch", nil)
			}
			return nil
		default:
			return camperrors.NewValidation("action", "branch accepts inherit, no-branch, or branch", nil)
		}
	default:
		switch action {
		case freshSettingInherit, freshSettingOn, freshSettingOff:
			return nil
		default:
			return camperrors.NewValidation("action", settingTitle(key)+" accepts inherit, on, or off", nil)
		}
	}
}

func settingStepFor(cfg *config.FreshConfig, projectName string, key freshSettingKey) freshWorkflowStep {
	for _, step := range buildFreshWorkflow(cfg, projectName) {
		if step.Kind == freshStepSetting && step.Setting == key {
			return step
		}
	}
	return freshWorkflowStep{Kind: freshStepSetting, Setting: key, GlobalOnly: key == freshSettingPrune || key == freshSettingPruneRemote}
}

func settingUnchanged(cfg *config.FreshConfig, projectName string, step freshWorkflowStep, action freshSettingAction, branch string) (bool, string) {
	if action != currentSettingAction(cfg, projectName, step) {
		return false, ""
	}
	if action == freshSettingCustomBranch && branch != settingScopeBranch(cfg, projectName) {
		return false, ""
	}
	title := settingTitle(step.Setting)
	switch action {
	case freshSettingInherit:
		if projectName != "" {
			return true, title + " already inherits · nothing written"
		}
		return true, title + " already at the built-in default · nothing written"
	case freshSettingOn:
		return true, title + " already on · nothing written"
	case freshSettingOff:
		return true, title + " already off · nothing written"
	case freshSettingNoBranch:
		return true, title + " already cleared · nothing written"
	case freshSettingCustomBranch:
		return true, title + " already " + settingScopeBranch(cfg, projectName) + " · nothing written"
	default:
		return true, title + " unchanged · nothing written"
	}
}

func writeFreshSetting(ctx context.Context, campRoot, projectName string, key freshSettingKey, action freshSettingAction, branch string) (string, error) {
	switch key {
	case freshSettingBranch:
		value, description, err := resolveBranchAction(action, branch)
		if err != nil {
			return "", err
		}
		if err := config.SetFreshBranch(ctx, campRoot, projectName, value); err != nil {
			return "", err
		}
		return description, nil
	case freshSettingPushUpstream:
		value := boolForAction(action)
		if err := config.SetFreshPushUpstream(ctx, campRoot, projectName, value); err != nil {
			return "", err
		}
		return settingOutcome("push_upstream", value, projectName != ""), nil
	case freshSettingPrune:
		value := boolForAction(action)
		if err := config.SetFreshPrune(ctx, campRoot, value); err != nil {
			return "", err
		}
		return settingOutcome("prune", value, false), nil
	case freshSettingPruneRemote:
		value := boolForAction(action)
		if err := config.SetFreshPruneRemote(ctx, campRoot, value); err != nil {
			return "", err
		}
		return settingOutcome("prune_remote", value, false), nil
	default:
		return "", camperrors.NewValidation("key", "is not a fresh setting", nil)
	}
}

func resolveBranchAction(action freshSettingAction, name string) (*string, string, error) {
	switch action {
	case freshSettingInherit:
		return nil, "branch now inherits the global default", nil
	case freshSettingNoBranch:
		empty := ""
		return &empty, "branch cleared · fresh stays on the default branch", nil
	default:
		if name == "" {
			return nil, "", camperrors.NewValidation("branch", "must not be empty; choose --action no-branch to stay on the default branch", nil)
		}
		return &name, fmt.Sprintf("branch set to %s", name), nil
	}
}
