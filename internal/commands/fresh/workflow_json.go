package fresh

import (
	"encoding/json"
	"io"
	"sort"

	"github.com/Obedience-Corp/camp/internal/config"
	"github.com/Obedience-Corp/camp/internal/project"
)

// JSONSchemaVersion is the contract for `camp fresh show-workflow --json`
// and `camp fresh configure set --json`.
const JSONSchemaVersion = "fresh-workflow/v1"

type freshWorkflowJSON struct {
	SchemaVersion     string           `json:"schema_version"`
	Project           string           `json:"project"`
	InheritsFollowUps bool             `json:"inherits_follow_ups"`
	Scopes            []freshScopeJSON `json:"scopes"`
	Steps             []freshStepJSON  `json:"steps"`
}

type freshScopeJSON struct {
	Project   string `json:"project"`
	Name      string `json:"name"`
	Overrides int    `json:"overrides"`
	Current   bool   `json:"current"`
}

type freshStepJSON struct {
	Title        string                `json:"title"`
	Detail       string                `json:"detail"`
	Enabled      bool                  `json:"enabled"`
	Kind         string                `json:"kind"`
	Section      string                `json:"section"`
	State        string                `json:"state"`
	Setting      string                `json:"setting,omitempty"`
	GlobalOnly   bool                  `json:"global_only,omitempty"`
	Configurable bool                  `json:"configurable"`
	Stored       string                `json:"stored,omitempty"`
	StoredBranch string                `json:"stored_branch,omitempty"`
	EditHint     string                `json:"edit_hint,omitempty"`
	Options      []freshSettingOptJSON `json:"options,omitempty"`
	Follow       *freshFollowJSON      `json:"follow,omitempty"`
}

type freshSettingOptJSON struct {
	Action string `json:"action"`
	Label  string `json:"label"`
}

type freshFollowJSON struct {
	Name            string `json:"name"`
	Run             string `json:"run"`
	Dir             string `json:"dir,omitempty"`
	ContinueOnError bool   `json:"continue_on_error"`
}

func emitFreshWorkflowJSON(w io.Writer, payload freshWorkflowJSON) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

func buildFreshWorkflowJSON(cfg *config.FreshConfig, projectName string, projects []project.Project) freshWorkflowJSON {
	if cfg == nil {
		cfg = &config.FreshConfig{}
	}
	projectScope := projectName != ""
	steps := buildFreshWorkflow(cfg, projectName)
	out := freshWorkflowJSON{
		SchemaVersion:     JSONSchemaVersion,
		Project:           projectName,
		InheritsFollowUps: projectScope && followUpsInherited(cfg, projectName),
		Scopes:            buildFreshScopesJSON(cfg, projects, projectName),
		Steps:             make([]freshStepJSON, 0, len(steps)),
	}
	for _, step := range steps {
		out.Steps = append(out.Steps, encodeFreshStep(cfg, projectName, step))
	}
	return out
}

func followUpsInherited(cfg *config.FreshConfig, projectName string) bool {
	if projectName == "" {
		return false
	}
	pc, ok := cfg.Projects[projectName]
	return !ok || pc.FollowUp == nil
}

func buildFreshScopesJSON(cfg *config.FreshConfig, projects []project.Project, current string) []freshScopeJSON {
	scopes := []freshScopeJSON{{
		Project: "",
		Name:    "Global defaults",
		Current: current == "",
	}}
	names := make(map[string]struct{}, len(projects)+len(cfg.Projects)+1)
	for _, p := range projects {
		names[p.Name] = struct{}{}
	}
	for name := range cfg.Projects {
		names[name] = struct{}{}
	}
	if current != "" {
		names[current] = struct{}{}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	for _, name := range ordered {
		scope := freshScopeJSON{
			Project: name,
			Name:    name,
			Current: name == current,
		}
		if pc, ok := cfg.Projects[name]; ok {
			scope.Overrides = config.ProjectOverrideKeys(pc)
		}
		scopes = append(scopes, scope)
	}
	return scopes
}

func encodeFreshStep(cfg *config.FreshConfig, projectName string, step freshWorkflowStep) freshStepJSON {
	projectScope := projectName != ""
	out := freshStepJSON{
		Title:        step.Title,
		Detail:       step.Detail,
		Enabled:      step.Enabled,
		Kind:         stepKindName(step.Kind),
		Section:      stepSectionName(step.Kind),
		State:        stepStateName(step.State),
		Setting:      settingTitle(step.Setting),
		GlobalOnly:   step.GlobalOnly,
		Configurable: step.Configurable(projectScope),
	}
	if step.Kind == freshStepSetting {
		out.Stored = settingActionName(currentSettingAction(cfg, projectName, step))
		if step.Setting == freshSettingBranch {
			out.StoredBranch = settingScopeBranch(cfg, projectName)
		}
		if out.Configurable {
			for _, option := range settingOptionsFor(cfg, projectName, step) {
				out.Options = append(out.Options, freshSettingOptJSON{
					Action: settingActionName(option.action),
					Label:  option.label,
				})
			}
		} else if step.GlobalOnly && projectScope {
			out.EditHint = settingTitle(step.Setting) + " is a camp-wide setting · select Global defaults to change it"
		}
	}
	if step.Follow != nil {
		out.Follow = &freshFollowJSON{
			Name:            step.Follow.Name,
			Run:             step.Follow.Run,
			Dir:             step.Follow.Dir,
			ContinueOnError: step.Follow.ContinueOnError,
		}
	}
	return out
}

func stepKindName(kind freshStepKind) string {
	switch kind {
	case freshStepSetting:
		return "setting"
	case freshStepFollowUp:
		return "follow_up"
	case freshStepDone:
		return "done"
	default:
		return "fixed"
	}
}

func stepSectionName(kind freshStepKind) string {
	switch kind {
	case freshStepSetting:
		return "settings"
	case freshStepFollowUp:
		return "follow_ups"
	case freshStepDone:
		return "done"
	default:
		return "sync"
	}
}

func stepStateName(state freshStepState) string {
	switch state {
	case freshStateOff:
		return "off"
	case freshStateUnset:
		return "unset"
	case freshStateBlocked:
		return "blocked"
	default:
		return "on"
	}
}
